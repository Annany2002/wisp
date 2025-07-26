package main

import (
	"bufio"
	"log"
	"net"
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
func handleConnection(conn net.Conn) {
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

	log.Printf("Request received: %+v", req)
	// Future header parsing and response logic will go here.
}

func main() {
	const address = ":8080"

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
		go handleConnection(conn)
	}
}
