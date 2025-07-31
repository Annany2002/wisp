package server

import (
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Annany2002/wisp/internal/config"
)

// sendErrorResponse writes a simple HTTP error response to the client.
func sendErrorResponse(conn net.Conn, statusCode int) {
	statusText := http.StatusText(statusCode)
	body := fmt.Sprintf("<html><body><h1>%d %s</h1></body></html>", statusCode, statusText)

	response := fmt.Sprintf(
		"HTTP/1.1 %d %s\r\n"+
			"Content-Type: text/html\r\n"+
			"Content-Length: %d\r\n"+
			"Date: %s\r\n"+
			"\r\n"+
			"%s",
		statusCode, statusText,
		len(body),
		time.Now().UTC().Format(time.RFC1123),
		body,
	)

	_, err := conn.Write([]byte(response))
	if err != nil {
		log.Printf("Failed to write error response: %v", err)
	}
}

// serveStaticFile serves a file from the filesystem.
func serveStaticFile(conn net.Conn, req *Request, loc *config.LocationConfig) {
	// Construct the full file path safely.
	path := filepath.Join(loc.Root, req.URI)

	// If the path is a directory, look for an index.html file.
	if stat, err := os.Stat(path); err == nil && stat.IsDir() {
		path = filepath.Join(path, "index.html")
	}

	// Try to open the file.
	file, err := os.Open(path)
	if err != nil {
		log.Printf("File not found: %s", path)
		sendErrorResponse(conn, http.StatusNotFound)
		return
	}
	defer file.Close()

	// Get file info for size.
	fileInfo, _ := file.Stat()
	fileSize := fileInfo.Size()

	// Determine the Content-Type header from the file extension.
	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType == "" {
		contentType = "application/octet-stream" // Default binary type
	}

	// Write headers
	responseHeaders := fmt.Sprintf(
		"HTTP/1.1 200 OK\r\n"+
			"Content-Type: %s\r\n"+
			"Content-Length: %d\r\n"+
			"Date: %s\r\n"+
			"\r\n",
		contentType,
		fileSize,
		time.Now().UTC().Format(time.RFC1123),
	)
	conn.Write([]byte(responseHeaders))

	// Stream the file content to the client.
	_, err = io.Copy(conn, file)
	if err != nil {
		log.Printf("Failed to write file content: %v", err)
	}
}

// serveReverseProxy forwards a request to a backend service.
func serveReverseProxy(conn net.Conn, req *Request, loc *config.LocationConfig) {
	backendURL, err := url.Parse(loc.ProxyPass)
	if err != nil {
		log.Printf("Malformed proxy_pass URL: %s", loc.ProxyPass)
		sendErrorResponse(conn, http.StatusInternalServerError)
		return
	}

	// Strip the location path from the request URI before forwarding.
	// e.g., a request to /api/users becomes /users for the backend.
	newURI := strings.TrimPrefix(req.URI, loc.Path)
	// Ensure the new URI starts with a slash.
	if !strings.HasPrefix(newURI, "/") {
		newURI = "/" + newURI
	}

	// Create a new request to the backend.
	// The backend receives the request URI from the original request.
	backendReq, err := http.NewRequest(req.Method, backendURL.String()+newURI, nil)
	if err != nil {
		log.Printf("Failed to create backend request: %v", err)
		sendErrorResponse(conn, http.StatusInternalServerError)
		return
	}

	// Copy headers from the original request to the new backend request.
	for key, value := range req.Headers {
		backendReq.Header.Set(key, value)
	}
	// Set the Host header to the backend's host.
	backendReq.Host = backendURL.Host

	// Send the request to the backend.
	backendResp, err := http.DefaultClient.Do(backendReq)
	if err != nil {
		log.Printf("Failed to get response from backend: %v", err)
		sendErrorResponse(conn, http.StatusBadGateway)
		return
	}
	defer backendResp.Body.Close()

	// --- Write the backend's response back to the original client ---
	// Write the status line from the backend response.
	conn.Write([]byte(fmt.Sprintf("%s %s\r\n", backendResp.Proto, backendResp.Status)))

	// Write headers from the backend response.
	for key, values := range backendResp.Header {
		for _, value := range values {
			conn.Write([]byte(fmt.Sprintf("%s: %s\r\n", key, value)))
		}
	}

	// Write the blank line separator.
	conn.Write([]byte("\r\n"))

	// Stream the backend's response body to the client.
	io.Copy(conn, backendResp.Body)
}
