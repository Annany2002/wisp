package server

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Annany2002/wisp/internal/config"
	"github.com/Annany2002/wisp/internal/upstream"
)

func TestIntegration(t *testing.T) {
	// 1. Setup Backend and Static Files
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo back the request body for POST requests.
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			fmt.Fprintf(w, "echo:%s", string(body))
			return
		}
		fmt.Fprintln(w, "Hello from backend")
	}))
	defer backend.Close()

	staticDir := t.TempDir()
	os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("Wisp static file"), 0644)

	// 2. Setup Wisp Server
	testPort := 8989
	cfg := &config.ServerConfig{
		Listen: testPort,
		Locations: []config.LocationConfig{
			{Path: "/", Root: staticDir},
			{Path: "/api/", ProxyPass: backend.URL},
		},
	}
	srv := New(cfg, nil)
	go srv.Start()
	time.Sleep(50 * time.Millisecond) // Give server time to start

	wispAddr := fmt.Sprintf("http://localhost:%d", testPort)

	// 3. Define Test Cases
	testCases := []struct {
		name           string
		path           string
		expectedStatus int
		expectedBody   string
	}{
		{"Static File Success", "/", http.StatusOK, "Wisp static file"},
		{"Static File Not Found", "/not-found.html", http.StatusNotFound, "Not Found"},
		{"Path Traversal Blocked", "/../../../etc/passwd", http.StatusNotFound, "Not Found"},
		{"Reverse Proxy Success", "/api/test", http.StatusOK, "Hello from backend"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(wispAddr + tc.path)
			if err != nil {
				t.Fatalf("Failed to send request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, resp.StatusCode)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("Failed to read response body: %v", err)
			}
			if !strings.Contains(string(body), tc.expectedBody) {
				t.Errorf("expected body to contain '%s', got '%s'", tc.expectedBody, string(body))
			}
		})
	}

	// Test gzip compression for static files.
	t.Run("Gzip Compression", func(t *testing.T) {
		req, _ := http.NewRequest("GET", wispAddr+"/", nil)
		req.Header.Set("Accept-Encoding", "gzip")

		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		if resp.Header.Get("Content-Encoding") != "gzip" {
			t.Errorf("expected Content-Encoding: gzip, got '%s'", resp.Header.Get("Content-Encoding"))
		}

		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			t.Fatalf("Failed to create gzip reader: %v", err)
		}
		defer gz.Close()

		body, _ := io.ReadAll(gz)
		if !strings.Contains(string(body), "Wisp static file") {
			t.Errorf("expected decompressed body to contain 'Wisp static file', got '%s'", string(body))
		}
	})

	// Test that non-gzip clients get uncompressed responses.
	t.Run("No Gzip Without Accept-Encoding", func(t *testing.T) {
		req, _ := http.NewRequest("GET", wispAddr+"/", nil)
		req.Header.Del("Accept-Encoding")

		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		if resp.Header.Get("Content-Encoding") == "gzip" {
			t.Error("did not expect gzip encoding when Accept-Encoding is absent")
		}

		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "Wisp static file") {
			t.Errorf("expected body to contain 'Wisp static file', got '%s'", string(body))
		}
	})

	// Test POST body forwarding through the reverse proxy.
	t.Run("Reverse Proxy POST Body", func(t *testing.T) {
		reqBody := `{"name":"wisp"}`
		resp, err := http.Post(wispAddr+"/api/echo", "application/json", strings.NewReader(reqBody))
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), "echo:"+reqBody) {
			t.Errorf("expected echoed body, got '%s'", string(body))
		}
	})
}

func TestLoadBalancingRoundRobin(t *testing.T) {
	// Create 3 backend servers, each identifying themselves.
	var hitCount [3]atomic.Int64
	backends := make([]*httptest.Server, 3)
	for i := range backends {
		idx := i
		backends[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hitCount[idx].Add(1)
			fmt.Fprintf(w, "backend-%d", idx)
		}))
		defer backends[i].Close()
	}

	// Build upstream config pointing to the 3 backends.
	backendAddrs := make([]config.UpstreamBackend, 3)
	for i, b := range backends {
		u, _ := url.Parse(b.URL)
		backendAddrs[i] = config.UpstreamBackend{Address: u.Host, Weight: 1}
	}

	upCfg := &config.UpstreamConfig{
		Name:     "testbackend",
		Method:   "round_robin",
		Backends: backendAddrs,
	}
	upstreams := map[string]*upstream.Upstream{
		"testbackend": upstream.New(upCfg),
	}

	testPort := 8991
	cfg := &config.ServerConfig{
		Listen: testPort,
		Locations: []config.LocationConfig{
			{Path: "/api/", ProxyPass: "http://testbackend"},
		},
	}
	srv := New(cfg, upstreams)
	go srv.Start()
	time.Sleep(50 * time.Millisecond)

	wispAddr := fmt.Sprintf("http://localhost:%d", testPort)

	// Send 6 requests — should round-robin evenly across 3 backends.
	responses := make([]string, 6)
	for i := range responses {
		resp, err := http.Get(wispAddr + "/api/test")
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		responses[i] = string(body)
	}

	// Verify each backend was hit exactly twice.
	for i, count := range hitCount {
		if count.Load() != 2 {
			t.Errorf("backend-%d: expected 2 hits, got %d", i, count.Load())
		}
	}

	// Verify round-robin order.
	expected := []string{"backend-0", "backend-1", "backend-2", "backend-0", "backend-1", "backend-2"}
	for i, resp := range responses {
		if !strings.Contains(resp, expected[i]) {
			t.Errorf("request %d: expected %s, got %s", i, expected[i], resp)
		}
	}
}

func TestLoadBalancingFailover(t *testing.T) {
	// Create 2 backends. Second one will be marked down.
	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "alive-backend")
	}))
	defer backend1.Close()

	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "dead-backend")
	}))
	// Close backend2 immediately to simulate it being down.
	backend2.Close()

	u1, _ := url.Parse(backend1.URL)
	u2, _ := url.Parse(backend2.URL)

	upCfg := &config.UpstreamConfig{
		Name:   "failover",
		Method: "round_robin",
		Backends: []config.UpstreamBackend{
			{Address: u1.Host, Weight: 1},
			{Address: u2.Host, Weight: 1},
		},
	}
	up := upstream.New(upCfg)
	// Mark the dead backend as down.
	up.Backends[1].Alive.Store(false)

	upstreams := map[string]*upstream.Upstream{"failover": up}

	testPort := 8992
	cfg := &config.ServerConfig{
		Listen: testPort,
		Locations: []config.LocationConfig{
			{Path: "/", ProxyPass: "http://failover"},
		},
	}
	srv := New(cfg, upstreams)
	go srv.Start()
	time.Sleep(50 * time.Millisecond)

	wispAddr := fmt.Sprintf("http://localhost:%d", testPort)

	// All requests should go to the alive backend.
	for i := 0; i < 4; i++ {
		resp, err := http.Get(wispAddr + "/test")
		if err != nil {
			t.Fatalf("Request %d failed: %v", i, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if !strings.Contains(string(body), "alive-backend") {
			t.Errorf("request %d: expected alive-backend, got %s", i, string(body))
		}
	}
}
