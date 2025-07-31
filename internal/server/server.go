package server

import (
	"bufio"
	"crypto/tls"
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

// Start runs the main listener loop, now with TLS capability.
func (s *Server) Start() error {
	var listener net.Listener
	var err error

	address := fmt.Sprintf(":%d", s.config.Listen)

	// Conditionally create either a TLS or a standard TCP listener.
	if s.config.SSLCertificate != "" && s.config.SSLCertificateKey != "" {
		// Load the key pair from the files specified in the config.
		cert, err := tls.LoadX509KeyPair(s.config.SSLCertificate, s.config.SSLCertificateKey)
		if err != nil {
			return fmt.Errorf("failed to load TLS key pair: %w", err)
		}

		tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}
		listener, err = tls.Listen("tcp", address, tlsConfig)
		log.Printf("Wisp secure server listening on %s", address)
	} else {
		listener, err = net.Listen("tcp", address)
		log.Printf("Wisp server listening on %s", address)
	}

	if err != nil {
		return fmt.Errorf("failed to start listener on %s: %w", address, err)
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}
		// Pass the server instance to the handler
		go s.handleConnection(conn)
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
		sendErrorResponse(conn, 404)
		return
	}
	if location.Root != "" {
		serveStaticFile(conn, &req, location)
	} else if location.ProxyPass != "" {
		serveReverseProxy(conn, &req, location)
	} else {
		sendErrorResponse(conn, 500)
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
