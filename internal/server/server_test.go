package server

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Annany2002/wisp/internal/config"
)

// setupTestEnvironment creates a full test environment and returns the Wisp server address and a cleanup function.
func setupTestEnvironment(t *testing.T) (string, func()) {
	// 1. Create a mock backend server that will be our proxy target.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello from backend")
	}))

	// 2. Create a temporary directory for static files.
	staticDir := t.TempDir()
	indexPath := filepath.Join(staticDir, "index.html")
	if err := os.WriteFile(indexPath, []byte("<h1>Wisp static file</h1>"), 0644); err != nil {
		t.Fatalf("Failed to write test index.html: %v", err)
	}

	// 3. Create a Wisp config programmatically for the test.
	cfg := &config.ServerConfig{
		Listen:     0, // Use 0 to let the OS pick a free port
		ServerName: "wisp.test",
		Locations: []config.LocationConfig{
			{Path: "/", Root: staticDir},
			{Path: "/api/", ProxyPass: backend.URL},
		},
	}

	// 4. Create and start our Wisp server.
	srv := New(cfg)
	// We need a way to get the random port it's listening on.
	// This is a common pattern for testing servers.
	go func() {
		if err := srv.Start(); err != nil {
			// Since this runs in a goroutine, we can't fail the test directly.
			// A real-world scenario might use channels to report errors.
			// For this test, logging is sufficient.
			log.Printf("Test server failed to start: %v", err)
		}
	}()

	// Give the server a moment to start.
	// In a more complex setup, you'd use channels or other synchronization.
	time.Sleep(50 * time.Millisecond)

	// Since we can't easily get the random port from the running server instance,
	// we will assume for this test that we can hardcode it or use a known one.
	// NOTE: For simplicity in this example, we will hardcode the test port.
	// A more advanced solution involves more complex synchronization.
	// Let's modify the config for a predictable port.
	cfg.Listen = 8989 // Use a unique port for testing.

	// The server address will be this known port.
	wispAddr := fmt.Sprintf("http://localhost:%d", cfg.Listen)

	// Return the server address and a cleanup function to stop the backend.
	return wispAddr, func() {
		backend.Close()
	}
}

func TestPathTraversalPrevention(t *testing.T) {
	// Create a parent directory with a secret file outside the static root.
	parentDir := t.TempDir()
	secretPath := filepath.Join(parentDir, "secret.txt")
	os.WriteFile(secretPath, []byte("TOP SECRET"), 0644)

	// The static root is a subdirectory — the secret file is outside it.
	staticDir := filepath.Join(parentDir, "public")
	os.MkdirAll(staticDir, 0755)
	os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("public page"), 0644)

	testPort := 8990
	cfg := &config.ServerConfig{
		Listen: testPort,
		Locations: []config.LocationConfig{
			{Path: "/", Root: staticDir},
		},
	}
	srv := New(cfg)
	go srv.Start()
	time.Sleep(50 * time.Millisecond)

	wispAddr := fmt.Sprintf("http://localhost:%d", testPort)

	// Attempt to read the secret file via path traversal.
	resp, err := http.Get(wispAddr + "/../secret.txt")
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "TOP SECRET") {
		t.Error("path traversal succeeded — secret file was served")
	}
	if resp.StatusCode == http.StatusOK {
		t.Errorf("expected non-200 status for traversal attempt, got %d", resp.StatusCode)
	}
}

func TestIntegration(t *testing.T) {
	// NOTE: The setup helper has a simplification for port handling.
	// In a real project, you would synchronize to get the random port.
	// For now, we will manually create and start the server here to control the port.

	// 1. Setup Backend and Static Files
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
}
