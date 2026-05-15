package tests

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"net"
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
	"github.com/Annany2002/wisp/internal/server"
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
	srv := server.New(cfg, nil)
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
	srv := server.New(cfg, upstreams)
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
	for i := range hitCount {
		if got := hitCount[i].Load(); got != 2 {
			t.Errorf("backend-%d: expected 2 hits, got %d", i, got)
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

func TestProxyHeaders(t *testing.T) {
	var captured http.Header
	var capturedHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		capturedHost = r.Host
		fmt.Fprint(w, "ok")
	}))
	defer backend.Close()

	testPort := 8993
	cfg := &config.ServerConfig{
		Listen: testPort,
		Locations: []config.LocationConfig{
			{
				Path:      "/",
				ProxyPass: backend.URL,
				ProxySetHeaders: []config.ProxyHeader{
					{Name: "Host", Value: "$host"},
					{Name: "X-Custom", Value: "wisp-$scheme"},
					{Name: "X-Real-IP", Value: "$remote_addr"},
					{Name: "X-Forwarded-For", Value: "$proxy_add_x_forwarded_for"},
				},
			},
		},
	}
	srv := server.New(cfg, nil)
	go srv.Start()
	defer srv.Shutdown()
	time.Sleep(50 * time.Millisecond)

	req, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/", testPort), nil)
	req.Host = "client.example.com"
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if capturedHost != "client.example.com" {
		t.Errorf("Host: expected client.example.com, got %q", capturedHost)
	}
	if captured.Get("X-Custom") != "wisp-http" {
		t.Errorf("X-Custom: expected wisp-http, got %q", captured.Get("X-Custom"))
	}
	if captured.Get("X-Forwarded-Proto") != "http" {
		t.Errorf("X-Forwarded-Proto: expected http, got %q", captured.Get("X-Forwarded-Proto"))
	}
	if captured.Get("X-Forwarded-Host") != "client.example.com" {
		t.Errorf("X-Forwarded-Host: expected client.example.com, got %q", captured.Get("X-Forwarded-Host"))
	}
	xff := captured.Get("X-Forwarded-For")
	if !strings.HasPrefix(xff, "10.0.0.1, ") {
		t.Errorf("X-Forwarded-For: expected chain starting with 10.0.0.1, got %q", xff)
	}
	realIP := captured.Get("X-Real-IP")
	if realIP == "" || realIP == "10.0.0.1" {
		t.Errorf("X-Real-IP: expected client conn IP, got %q", realIP)
	}
}

func TestProxyStripsHopByHop(t *testing.T) {
	var captured http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.Header().Set("Connection", "close")
		w.Header().Set("Keep-Alive", "timeout=5")
		fmt.Fprint(w, "ok")
	}))
	defer backend.Close()

	testPort := 8994
	cfg := &config.ServerConfig{
		Listen: testPort,
		Locations: []config.LocationConfig{
			{Path: "/", ProxyPass: backend.URL},
		},
	}
	srv := server.New(cfg, nil)
	go srv.Start()
	defer srv.Shutdown()
	time.Sleep(50 * time.Millisecond)

	req, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/", testPort), nil)
	req.Header.Set("Proxy-Authorization", "Bearer leak")
	req.Header.Set("X-Keep", "yes")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if captured.Get("Proxy-Authorization") != "" {
		t.Error("Proxy-Authorization should be stripped from forwarded request")
	}
	if captured.Get("X-Keep") != "yes" {
		t.Error("non-hop-by-hop header X-Keep should be forwarded")
	}
	if resp.Header.Get("Keep-Alive") != "" {
		t.Error("Keep-Alive should be stripped from response")
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
	srv := server.New(cfg, upstreams)
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

func TestAddHeaderOnStaticResponse(t *testing.T) {
	staticDir := t.TempDir()
	os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("hi"), 0644)

	testPort := 9001
	cfg := &config.ServerConfig{
		Listen: testPort,
		Locations: []config.LocationConfig{
			{
				Path: "/",
				Root: staticDir,
				AddHeaders: []config.ProxyHeader{
					{Name: "X-Frame-Options", Value: "DENY"},
					{Name: "Strict-Transport-Security", Value: "max-age=63072000"},
				},
			},
		},
	}
	srv := server.New(cfg, nil)
	go srv.Start()
	defer srv.Shutdown()
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/", testPort))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options: expected DENY, got %q", got)
	}
	if got := resp.Header.Get("Strict-Transport-Security"); got != "max-age=63072000" {
		t.Errorf("HSTS: expected max-age=63072000, got %q", got)
	}
}

func TestAddHeaderOnGzipResponse(t *testing.T) {
	staticDir := t.TempDir()
	os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("compress me"), 0644)

	testPort := 9002
	cfg := &config.ServerConfig{
		Listen: testPort,
		Locations: []config.LocationConfig{
			{
				Path: "/",
				Root: staticDir,
				AddHeaders: []config.ProxyHeader{
					{Name: "X-Custom", Value: "gzip-path"},
				},
			},
		},
	}
	srv := server.New(cfg, nil)
	go srv.Start()
	defer srv.Shutdown()
	time.Sleep(50 * time.Millisecond)

	req, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost:%d/", testPort), nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Errorf("expected gzip response, got Content-Encoding=%q", resp.Header.Get("Content-Encoding"))
	}
	if got := resp.Header.Get("X-Custom"); got != "gzip-path" {
		t.Errorf("X-Custom: expected gzip-path, got %q", got)
	}
}

func TestAddHeaderOnProxyResponse(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "yes")
		fmt.Fprint(w, "from backend")
	}))
	defer backend.Close()

	testPort := 9003
	cfg := &config.ServerConfig{
		Listen: testPort,
		Locations: []config.LocationConfig{
			{
				Path:      "/",
				ProxyPass: backend.URL,
				AddHeaders: []config.ProxyHeader{
					{Name: "X-Frame-Options", Value: "SAMEORIGIN"},
				},
			},
		},
	}
	srv := server.New(cfg, nil)
	go srv.Start()
	defer srv.Shutdown()
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/", testPort))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options: expected SAMEORIGIN, got %q", got)
	}
	if got := resp.Header.Get("X-Backend"); got != "yes" {
		t.Errorf("backend header lost: expected yes, got %q", got)
	}
}

func TestProxyRetryOnConnectionFailure(t *testing.T) {
	var hits atomic.Int32
	alive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, "served-by-alive")
	}))
	defer alive.Close()

	// A backend that's never reachable: bind a listener then close it. Its
	// address is recorded but TCP dials will refuse.
	deadLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	deadAddr := deadLn.Addr().String()
	deadLn.Close()

	au, _ := url.Parse(alive.URL)
	upCfg := &config.UpstreamConfig{
		Name:   "retrygroup",
		Method: "round_robin",
		Backends: []config.UpstreamBackend{
			{Address: deadAddr, Weight: 1}, // refuses connection
			{Address: au.Host, Weight: 1},  // alive
		},
		// No health check → backends start as Alive=true; retry must
		// discover the dead one at request time.
	}
	up := upstream.New(upCfg)
	upstreams := map[string]*upstream.Upstream{"retrygroup": up}

	testPort := 8997
	cfg := &config.ServerConfig{
		Listen: testPort,
		Locations: []config.LocationConfig{
			{Path: "/", ProxyPass: "http://retrygroup"},
		},
	}
	srv := server.New(cfg, upstreams)
	go srv.Start()
	defer srv.Shutdown()
	time.Sleep(50 * time.Millisecond)

	wispAddr := fmt.Sprintf("http://localhost:%d", testPort)

	// First request: round-robin picks the dead backend first; retry should
	// fall through to the alive one without surfacing a 502.
	for i := 0; i < 4; i++ {
		resp, err := http.Get(wispAddr + "/test")
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, resp.StatusCode)
		}
		if !strings.Contains(string(body), "served-by-alive") {
			t.Errorf("request %d: expected served-by-alive, got %q", i, string(body))
		}
	}

	if hits.Load() != 4 {
		t.Errorf("alive backend hits: expected 4, got %d", hits.Load())
	}

	// Dead backend should have been marked unhealthy after first failure.
	if up.Backends[0].Alive.Load() {
		t.Error("dead backend should be marked Alive=false after retry")
	}
}

func TestProxyRetryReplaysRequestBody(t *testing.T) {
	var receivedBody atomic.Value
	alive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		receivedBody.Store(string(b))
		fmt.Fprint(w, "ok")
	}))
	defer alive.Close()

	deadLn, _ := net.Listen("tcp", "127.0.0.1:0")
	deadAddr := deadLn.Addr().String()
	deadLn.Close()

	au, _ := url.Parse(alive.URL)
	upCfg := &config.UpstreamConfig{
		Name:   "bodyretry",
		Method: "round_robin",
		Backends: []config.UpstreamBackend{
			{Address: deadAddr, Weight: 1},
			{Address: au.Host, Weight: 1},
		},
	}
	upstreams := map[string]*upstream.Upstream{"bodyretry": upstream.New(upCfg)}

	testPort := 8998
	cfg := &config.ServerConfig{
		Listen: testPort,
		Locations: []config.LocationConfig{
			{Path: "/", ProxyPass: "http://bodyretry"},
		},
	}
	srv := server.New(cfg, upstreams)
	go srv.Start()
	defer srv.Shutdown()
	time.Sleep(50 * time.Millisecond)

	payload := `{"id":42,"name":"wisp"}`
	resp, err := http.Post(fmt.Sprintf("http://localhost:%d/echo", testPort), "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	resp.Body.Close()

	if got, _ := receivedBody.Load().(string); got != payload {
		t.Errorf("body replay: expected %q, got %q", payload, got)
	}
}

func TestProxyNoRetryOnBackend5xx(t *testing.T) {
	var hits atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "boom")
	}))
	defer backend.Close()

	bu, _ := url.Parse(backend.URL)
	upCfg := &config.UpstreamConfig{
		Name:   "noretry",
		Method: "round_robin",
		Backends: []config.UpstreamBackend{
			{Address: bu.Host, Weight: 1},
			{Address: bu.Host, Weight: 1},
		},
	}
	upstreams := map[string]*upstream.Upstream{"noretry": upstream.New(upCfg)}

	testPort := 8999
	cfg := &config.ServerConfig{
		Listen: testPort,
		Locations: []config.LocationConfig{
			{Path: "/", ProxyPass: "http://noretry"},
		},
	}
	srv := server.New(cfg, upstreams)
	go srv.Start()
	defer srv.Shutdown()
	time.Sleep(50 * time.Millisecond)

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/", testPort))
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500 forwarded verbatim, got %d", resp.StatusCode)
	}
	if hits.Load() != 1 {
		t.Errorf("backend hits: expected 1 (no retry on 5xx), got %d", hits.Load())
	}
}

// wsEchoBackend hijacks the connection, completes a 101 handshake, and
// echoes any received bytes back to the client. The bytes do not have to
// be valid WS frames — the proxy is protocol-agnostic after upgrade.
func wsEchoBackend(t *testing.T, capturedHeaders chan<- http.Header) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capturedHeaders != nil {
			capturedHeaders <- r.Header.Clone()
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("backend ResponseWriter does not support Hijack")
			return
		}
		conn, brw, err := hj.Hijack()
		if err != nil {
			t.Errorf("backend hijack failed: %v", err)
			return
		}
		defer conn.Close()

		resp := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: dummy\r\n\r\n"
		if _, err := brw.WriteString(resp); err != nil {
			return
		}
		if err := brw.Flush(); err != nil {
			return
		}

		// Echo loop: copy client bytes back until close.
		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				if _, werr := conn.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}))
}

func TestWebSocketProxyHandshakeAndEcho(t *testing.T) {
	headersCh := make(chan http.Header, 1)
	backend := wsEchoBackend(t, headersCh)
	defer backend.Close()

	testPort := 8995
	cfg := &config.ServerConfig{
		Listen: testPort,
		Locations: []config.LocationConfig{
			{Path: "/ws/", ProxyPass: backend.URL},
		},
	}
	srv := server.New(cfg, nil)
	go srv.Start()
	defer srv.Shutdown()
	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", testPort))
	if err != nil {
		t.Fatalf("dial wisp: %v", err)
	}
	defer conn.Close()

	handshake := "GET /ws/chat HTTP/1.1\r\n" +
		"Host: client.example.com\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := conn.Write([]byte(handshake)); err != nil {
		t.Fatalf("send handshake: %v", err)
	}

	r := bufio.NewReader(conn)
	statusLine, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	if !strings.HasPrefix(statusLine, "HTTP/1.1 101") {
		t.Fatalf("expected 101 Switching Protocols, got %q", statusLine)
	}

	// Drain remaining response headers.
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read header: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}

	// Backend should have received WS handshake headers and X-Forwarded-*.
	var bh http.Header
	select {
	case bh = <-headersCh:
	case <-time.After(time.Second):
		t.Fatal("backend never received request")
	}
	if !strings.EqualFold(bh.Get("Upgrade"), "websocket") {
		t.Errorf("backend Upgrade header: expected websocket, got %q", bh.Get("Upgrade"))
	}
	if !strings.Contains(strings.ToLower(bh.Get("Connection")), "upgrade") {
		t.Errorf("backend Connection header: expected upgrade, got %q", bh.Get("Connection"))
	}
	if bh.Get("X-Forwarded-Proto") != "http" {
		t.Errorf("X-Forwarded-Proto: expected http, got %q", bh.Get("X-Forwarded-Proto"))
	}
	if bh.Get("X-Real-IP") == "" {
		t.Error("X-Real-IP not injected")
	}

	// Bidirectional splice: send bytes, expect echo.
	if _, err := conn.Write([]byte("ping-from-client")); err != nil {
		t.Fatalf("send ws payload: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	got := make([]byte, len("ping-from-client"))
	if _, err := io.ReadFull(r, got); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(got) != "ping-from-client" {
		t.Errorf("echo mismatch: got %q", string(got))
	}
}

func TestWebSocketProxyBackendDeclines(t *testing.T) {
	// Backend that rejects the upgrade with a 400.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no upgrade for you", http.StatusBadRequest)
	}))
	defer backend.Close()

	testPort := 8996
	cfg := &config.ServerConfig{
		Listen: testPort,
		Locations: []config.LocationConfig{
			{Path: "/ws/", ProxyPass: backend.URL},
		},
	}
	srv := server.New(cfg, nil)
	go srv.Start()
	defer srv.Shutdown()
	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", testPort))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	handshake := "GET /ws/x HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n\r\n"
	if _, err := conn.Write([]byte(handshake)); err != nil {
		t.Fatalf("send: %v", err)
	}

	r := bufio.NewReader(conn)
	statusLine, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasPrefix(statusLine, "HTTP/1.1 400") {
		t.Fatalf("expected 400 from declined upgrade, got %q", statusLine)
	}
}
