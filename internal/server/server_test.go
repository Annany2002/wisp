package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Annany2002/wisp/internal/config"
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
	srv := New(cfg)
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
