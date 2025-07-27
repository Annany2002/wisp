package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
)

// Request holds the parsed data from an HTTP request line.
type Request struct {
	Method  string
	URI     string
	Version string
	Headers map[string]string
}

// handleConnection manages a single client connection.
func handleConnection(conn net.Conn, config *ServerConfig) {
	defer conn.Close()
	log.Printf("Handling connection from %s", conn.RemoteAddr())

	// Use a buffered reader for efficient I/O.
	reader := bufio.NewReader(conn)

	// Read the first line from the connection, which is the request line.
	requestLine, err := reader.ReadString('\n')
	if err != nil {
		log.Printf("Failed to read request line: %v", err)
		return
	}

	// Parse the request line.
	parts := strings.Fields(requestLine)
	if len(parts) != 3 {
		log.Printf("Malformed request line: %s", requestLine)
		return
	}

	req := Request{
		Method:  parts[0],
		URI:     parts[1],
		Version: parts[2],
		Headers: make(map[string]string),
	}

	// Loop to read and parse headers until a blank line is found
	for {
		headerLine, err := reader.ReadString('\n')
		if err != nil {
			log.Printf("Failed to read header: %v", err)
			return
		}

		// A blank line (\r\n) signifies line break
		if headerLine == "\r\n" {
			break
		}

		// Split the header line into key and value
		headerParts := strings.SplitN(headerLine, ":", 2)
		if len(headerParts) != 2 {
			log.Printf("Malformed header: %s", headerLine)
			continue
		}

		// Extract the key and value
		key := strings.TrimSpace(headerParts[0])
		value := strings.TrimSpace(headerParts[1])
		req.Headers[key] = value
	}

	// Use the router to find the correct location for this request.
	location := RouteRequest(&req, config)

	// Dispatch to the appropriate handler.
	if location == nil {
		sendErrorResponse(conn, http.StatusNotFound)
		return
	}

	if location.Root != "" {
		ServeStaticFile(conn, &req, location)
	} else if location.ProxyPass != "" {
		// Future proxy logic will go here.
		log.Printf("Proxy pass not yet implemented for %s", location.Path)
		sendErrorResponse(conn, http.StatusNotImplemented)
	} else {
		log.Printf("Location %s is not configured for any action", location.Path)
		sendErrorResponse(conn, http.StatusInternalServerError)
	}
}

func main() {
	config, err := ParseConfig("wisp.conf")
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// using the port from the config file
	address := fmt.Sprintf(":%d", config.Listen)

	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
	defer listener.Close()

	log.Printf("Wisp server listening on %s", address)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}
		go handleConnection(conn, config)
	}
}
