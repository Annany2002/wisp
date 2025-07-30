package server

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/Annany2002/wisp/internal/config"
)

// Server represents the Wisp server instance.
type Server struct {
	config *config.ServerConfig
}

// Request holds parsed HTTP request data.
type Request struct {
	Method  string
	URI     string
	Version string
	Headers map[string]string
}

// New creates a new Wisp server instance.
func New(cfg *config.ServerConfig) *Server {
	return &Server{config: cfg}
}

// Start runs the main TCP listener loop.
func (s *Server) Start() error {
	address := fmt.Sprintf(":%d", s.config.Listen)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to start server on port %d: %w", s.config.Listen, err)
	}
	defer listener.Close()

	log.Printf("Wisp server listening on %s", address)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}
		go s.handleConnection(conn) // Pass the server instance to the handler
	}
}

// handleConnection now uses the server's config field 's.config'.
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Use a buffered reader for efficient I/O.
	reader := bufio.NewReader(conn)

	// Read the first line from the connection, which is the request line.
	requestLine, _ := reader.ReadString('\n')
	parts := strings.Fields(requestLine)
	if len(parts) != 3 {
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
	location := s.routeRequest(&req)

	// Dispatch to handlers
	if location == nil {
		SendErrorResponse(conn, 404)
		return
	}
	if location.Root != "" {
		ServeStaticFile(conn, &req, location)
	} else if location.ProxyPass != "" {
		serveReverseProxy(conn, &req, location)
	} else {
		SendErrorResponse(conn, 500)
	}
}

// routeRequest finds the best location configuration for a given request.
// It uses a longest prefix matching algorithm.
func (s *Server) routeRequest(req *Request) *config.LocationConfig {
	var bestMatch *config.LocationConfig
	longestMatchLen := 0
	for i, location := range s.config.Locations {
		if strings.HasPrefix(req.URI, location.Path) {
			if len(location.Path) > longestMatchLen {
				longestMatchLen = len(location.Path)
				bestMatch = &s.config.Locations[i]
			}
		}
	}
	return bestMatch
}
