package tests

import (
	"testing"

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
