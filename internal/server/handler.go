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
// Returns the HTTP status code sent to the client.
func serveStaticFile(conn net.Conn, req *Request, loc *config.LocationConfig) int {
	// Resolve the root to an absolute path for safe comparison.
	absRoot, err := filepath.Abs(loc.Root)
	if err != nil {
		log.Printf("Failed to resolve root path: %v", err)
		sendErrorResponse(conn, http.StatusInternalServerError)
		return http.StatusInternalServerError
	}

	// Clean the URI and join with root to get the target path.
	cleanURI := filepath.Clean(req.URI)
	path := filepath.Join(absRoot, cleanURI)

	// Prevent path traversal: ensure the resolved path is within the root.
	if !strings.HasPrefix(path, absRoot+string(filepath.Separator)) && path != absRoot {
		log.Printf("Path traversal attempt blocked: %s", req.URI)
		sendErrorResponse(conn, http.StatusForbidden)
		return http.StatusForbidden
	}

	// If the path is a directory, look for an index.html file.
	if stat, err := os.Stat(path); err == nil && stat.IsDir() {
		path = filepath.Join(path, "index.html")
	}

	// Try to open the file.
	file, err := os.Open(path)
	if err != nil {
		sendErrorResponse(conn, http.StatusNotFound)
		return http.StatusNotFound
	}
	defer file.Close()

	// Get file info for size.
	fileInfo, _ := file.Stat()
	fileSize := fileInfo.Size()

	// Determine the Content-Type header from the file extension.
	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Write headers.
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
	return http.StatusOK
}

// serveReverseProxy forwards a request to a backend service.
// Returns the HTTP status code sent to the client.
func serveReverseProxy(conn net.Conn, req *Request, loc *config.LocationConfig) int {
	backendURL, err := url.Parse(loc.ProxyPass)
	if err != nil {
		log.Printf("Malformed proxy_pass URL: %s", loc.ProxyPass)
		sendErrorResponse(conn, http.StatusInternalServerError)
		return http.StatusInternalServerError
	}

	// Strip the location path from the request URI before forwarding.
	newURI := strings.TrimPrefix(req.URI, loc.Path)
	if !strings.HasPrefix(newURI, "/") {
		newURI = "/" + newURI
	}

	// Create a new request to the backend, forwarding the body if present.
	backendReq, err := http.NewRequest(req.Method, backendURL.String()+newURI, req.Body)
	if err != nil {
		log.Printf("Failed to create backend request: %v", err)
		sendErrorResponse(conn, http.StatusInternalServerError)
		return http.StatusInternalServerError
	}

	// Copy headers from the original request to the new backend request.
	for key, value := range req.Headers {
		backendReq.Header.Set(key, value)
	}
	backendReq.Host = backendURL.Host

	// Send the request to the backend.
	backendResp, err := http.DefaultClient.Do(backendReq)
	if err != nil {
		log.Printf("Failed to get response from backend: %v", err)
		sendErrorResponse(conn, http.StatusBadGateway)
		return http.StatusBadGateway
	}
	defer backendResp.Body.Close()

	// Write the backend's response back to the original client.
	conn.Write([]byte(fmt.Sprintf("%s %s\r\n", backendResp.Proto, backendResp.Status)))

	for key, values := range backendResp.Header {
		for _, value := range values {
			conn.Write([]byte(fmt.Sprintf("%s: %s\r\n", key, value)))
		}
	}

	conn.Write([]byte("\r\n"))
	io.Copy(conn, backendResp.Body)

	return backendResp.StatusCode
}
