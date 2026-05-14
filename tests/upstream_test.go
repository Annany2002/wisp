package tests

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Annany2002/wisp/internal/config"
	"github.com/Annany2002/wisp/internal/upstream"
)

func TestRoundRobin(t *testing.T) {
	cfg := &config.UpstreamConfig{
		Name:   "test",
		Method: "round_robin",
		Backends: []config.UpstreamBackend{
			{Address: "127.0.0.1:3001", Weight: 1},
			{Address: "127.0.0.1:3002", Weight: 1},
			{Address: "127.0.0.1:3003", Weight: 1},
		},
	}
	u := upstream.New(cfg)

	// Should cycle through backends in order.
	addrs := make([]string, 6)
	for i := range addrs {
		b, err := u.Next()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		addrs[i] = b.Address
	}

	expected := []string{
		"127.0.0.1:3001", "127.0.0.1:3002", "127.0.0.1:3003",
		"127.0.0.1:3001", "127.0.0.1:3002", "127.0.0.1:3003",
	}
	for i, addr := range addrs {
		if addr != expected[i] {
			t.Errorf("request %d: expected %s, got %s", i, expected[i], addr)
		}
	}
}

func TestRoundRobinSkipsUnhealthy(t *testing.T) {
	cfg := &config.UpstreamConfig{
		Name:   "test",
		Method: "round_robin",
		Backends: []config.UpstreamBackend{
			{Address: "127.0.0.1:3001", Weight: 1},
			{Address: "127.0.0.1:3002", Weight: 1},
			{Address: "127.0.0.1:3003", Weight: 1},
		},
	}
	u := upstream.New(cfg)

	// Mark second backend as down.
	u.Backends[1].Alive.Store(false)

	addrs := make([]string, 4)
	for i := range addrs {
		b, err := u.Next()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		addrs[i] = b.Address
	}

	for _, addr := range addrs {
		if addr == "127.0.0.1:3002" {
			t.Error("unhealthy backend should not be selected")
		}
	}
}

func TestAllBackendsDown(t *testing.T) {
	cfg := &config.UpstreamConfig{
		Name:   "test",
		Method: "round_robin",
		Backends: []config.UpstreamBackend{
			{Address: "127.0.0.1:3001", Weight: 1},
		},
	}
	u := upstream.New(cfg)
	u.Backends[0].Alive.Store(false)

	_, err := u.Next()
	if err == nil {
		t.Error("expected error when all backends are down, got nil")
	}
}

func TestLeastConn(t *testing.T) {
	cfg := &config.UpstreamConfig{
		Name:   "test",
		Method: "least_conn",
		Backends: []config.UpstreamBackend{
			{Address: "127.0.0.1:3001", Weight: 1},
			{Address: "127.0.0.1:3002", Weight: 1},
			{Address: "127.0.0.1:3003", Weight: 1},
		},
	}
	u := upstream.New(cfg)

	// Simulate connections: backend 1 has 5, backend 2 has 2, backend 3 has 10.
	u.Backends[0].ActiveConns.Store(5)
	u.Backends[1].ActiveConns.Store(2)
	u.Backends[2].ActiveConns.Store(10)

	b, err := u.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Address != "127.0.0.1:3002" {
		t.Errorf("expected least-conn to pick 127.0.0.1:3002, got %s", b.Address)
	}
}

func TestLeastConnWithWeight(t *testing.T) {
	cfg := &config.UpstreamConfig{
		Name:   "test",
		Method: "least_conn",
		Backends: []config.UpstreamBackend{
			{Address: "127.0.0.1:3001", Weight: 1}, // 5 conns / weight 1 = load 5.0
			{Address: "127.0.0.1:3002", Weight: 5}, // 10 conns / weight 5 = load 2.0
		},
	}
	u := upstream.New(cfg)

	u.Backends[0].ActiveConns.Store(5)
	u.Backends[1].ActiveConns.Store(10)

	b, err := u.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Backend 2 has more absolute conns but lower effective load.
	if b.Address != "127.0.0.1:3002" {
		t.Errorf("expected weighted least-conn to pick 127.0.0.1:3002, got %s", b.Address)
	}
}

func TestLeastConnSkipsUnhealthy(t *testing.T) {
	cfg := &config.UpstreamConfig{
		Name:   "test",
		Method: "least_conn",
		Backends: []config.UpstreamBackend{
			{Address: "127.0.0.1:3001", Weight: 1},
			{Address: "127.0.0.1:3002", Weight: 1},
		},
	}
	u := upstream.New(cfg)

	// Backend 1 has 0 conns but is down. Backend 2 has 5 conns but is alive.
	u.Backends[0].ActiveConns.Store(0)
	u.Backends[0].Alive.Store(false)
	u.Backends[1].ActiveConns.Store(5)

	b, err := u.Next()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Address != "127.0.0.1:3002" {
		t.Errorf("expected healthy backend, got %s", b.Address)
	}
}

// runHealthCycle drives one health check pass synchronously. StartHealthChecks
// only fires on ticker, so tests poke the unexported checkBackends path via a
// short interval and wait. To avoid time-based flakes, the tests instead drive
// behavior by toggling the backend response and waiting on the Alive flag.
func runHealthCycle(t *testing.T, u *upstream.Upstream, want bool, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, b := range u.Backends {
			if b.Address == addr && b.Alive.Load() == want {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("backend %s did not reach alive=%v within deadline", addr, want)
}

func TestHTTPHealthCheckMarksUnhealthyOn500(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusOK)
	probeHits := atomic.Int64{}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			probeHits.Add(1)
			w.WriteHeader(int(status.Load()))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	bu, _ := url.Parse(backend.URL)
	cfg := &config.UpstreamConfig{
		Name:   "hctest",
		Method: "round_robin",
		Backends: []config.UpstreamBackend{
			{Address: bu.Host, Weight: 1},
		},
		HealthCheck: &config.HealthCheckConfig{
			Path:     "/healthz",
			Interval: 100 * time.Millisecond,
			Timeout:  500 * time.Millisecond,
			Expect:   200,
		},
	}
	u := upstream.New(cfg)
	stop := u.StartHealthChecks()
	defer stop()

	// Wait one tick to ensure at least one probe fired while healthy.
	runHealthCycle(t, u, true, bu.Host)

	// Flip backend to return 500 — health check should mark it down.
	status.Store(http.StatusInternalServerError)
	runHealthCycle(t, u, false, bu.Host)

	// Flip back to 200 — should recover.
	status.Store(http.StatusOK)
	runHealthCycle(t, u, true, bu.Host)

	if probeHits.Load() == 0 {
		t.Error("expected health probes to hit /healthz")
	}
}

func TestHTTPHealthCheckRespectsExpectStatus(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent) // 204
	}))
	defer backend.Close()

	bu, _ := url.Parse(backend.URL)
	cfg := &config.UpstreamConfig{
		Name:   "hctest",
		Method: "round_robin",
		Backends: []config.UpstreamBackend{
			{Address: bu.Host, Weight: 1},
		},
		HealthCheck: &config.HealthCheckConfig{
			Path:     "/",
			Interval: 100 * time.Millisecond,
			Timeout:  500 * time.Millisecond,
			Expect:   204,
		},
	}
	u := upstream.New(cfg)
	stop := u.StartHealthChecks()
	defer stop()

	// Backend returns 204 which matches Expect — backend should stay alive.
	runHealthCycle(t, u, true, bu.Host)

	// Now reconfigure expectation to 200 by swapping upstream — verifies that
	// a mismatch flips Alive to false.
	cfg2 := *cfg
	hc2 := *cfg.HealthCheck
	hc2.Expect = 200
	cfg2.HealthCheck = &hc2
	u2 := upstream.New(&cfg2)
	stop2 := u2.StartHealthChecks()
	defer stop2()

	runHealthCycle(t, u2, false, bu.Host)
}

func TestTCPHealthCheckFallbackWhenNoPath(t *testing.T) {
	// Listener with no HTTP behavior — TCP dial succeeds, anything else fails.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	cfg := &config.UpstreamConfig{
		Name:   "tcponly",
		Method: "round_robin",
		Backends: []config.UpstreamBackend{
			{Address: ln.Addr().String(), Weight: 1},
		},
		// Empty Path → TCP probe; Interval/Timeout shorten the loop for tests.
		HealthCheck: &config.HealthCheckConfig{
			Interval: 100 * time.Millisecond,
			Timeout:  300 * time.Millisecond,
		},
	}
	u := upstream.New(cfg)
	stop := u.StartHealthChecks()
	defer stop()

	// Should remain alive — TCP accept works.
	time.Sleep(150 * time.Millisecond)
	if !u.Backends[0].Alive.Load() {
		t.Error("TCP-probable backend should be alive")
	}

	// Close the listener — next probe should mark backend down.
	ln.Close()
	runHealthCycle(t, u, false, ln.Addr().String())
}
