package main

import (
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
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
func ServeStaticFile(conn net.Conn, req *Request, loc *LocationConfig) {
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
