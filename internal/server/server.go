package server

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Annany2002/wisp/internal/config"
	"github.com/Annany2002/wisp/internal/upstream"
)

const (
	// readTimeout is the maximum duration for reading the entire request.
	readTimeout = 30 * time.Second
	// writeTimeout is the maximum duration for writing the response.
	writeTimeout = 30 * time.Second
	// keepAliveTimeout is the maximum idle time between requests on a persistent connection.
	keepAliveTimeout = 60 * time.Second
)

// Server represents the Wisp server instance.
type Server struct {
	mu        sync.RWMutex
	config    *config.ServerConfig
	listener  net.Listener
	upstreams map[string]*upstream.Upstream
	tlsCert   *tls.Certificate
}

// Request holds parsed HTTP request data.
type Request struct {
	Method  string
	URI     string
	Version string
	Headers map[string]string
	Body    io.Reader
}

// New creates a new Wisp server instance with optional upstream groups.
func New(cfg *config.ServerConfig, upstreams map[string]*upstream.Upstream) *Server {
	return &Server{config: cfg, upstreams: upstreams}
}

// Start runs the main listener loop with TLS capability.
func (s *Server) Start() error {
	var err error

	address := fmt.Sprintf(":%d", s.config.Listen)

	// Conditionally create either a TLS or a standard TCP listener.
	if s.config.SSLCertificate != "" && s.config.SSLCertificateKey != "" {
		cert, err := tls.LoadX509KeyPair(s.config.SSLCertificate, s.config.SSLCertificateKey)
		if err != nil {
			return fmt.Errorf("failed to load TLS key pair: %w", err)
		}
		s.tlsCert = &cert

		tlsConfig := &tls.Config{
			GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
				s.mu.RLock()
				defer s.mu.RUnlock()
				if s.tlsCert == nil {
					return nil, fmt.Errorf("no certificate loaded")
				}
				return s.tlsCert, nil
			},
		}
		s.listener, err = tls.Listen("tcp", address, tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to start TLS listener on %s: %w", address, err)
		}
		log.Printf("Wisp secure server listening on %s", address)
	} else {
		s.listener, err = net.Listen("tcp", address)
		if err != nil {
			return fmt.Errorf("failed to start listener on %s: %w", address, err)
		}
		log.Printf("Wisp server listening on %s", address)
	}

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// Check if the error is due to the listener being closed (shutdown).
			if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
				log.Println("Listener closed, shutting down accept loop")
				return nil
			}
			log.Printf("Failed to accept connection: %v", err)
			continue
		}
		go s.handleConnection(conn)
	}
}

// scheme returns "https" when TLS is configured, else "http".
func (s *Server) scheme() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.config.SSLCertificate != "" && s.config.SSLCertificateKey != "" {
		return "https"
	}
	return "http"
}

// Shutdown gracefully stops the server by closing the listener.
func (s *Server) Shutdown() error {
	if s.listener != nil {
		log.Println("Shutting down Wisp server...")
		return s.listener.Close()
	}
	return nil
}

// UpdateConfig dynamically updates the server configuration, upstream routing map,
// and TLS certificate on the fly.
func (s *Server) UpdateConfig(cfg *config.ServerConfig, upstreams map[string]*upstream.Upstream) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config = cfg
	s.upstreams = upstreams

	if cfg.SSLCertificate != "" && cfg.SSLCertificateKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.SSLCertificate, cfg.SSLCertificateKey)
		if err != nil {
			return fmt.Errorf("failed to reload TLS key pair: %w", err)
		}
		s.tlsCert = &cert
	} else {
		s.tlsCert = nil
	}

	return nil
}

// HasSSL returns true if the server is configured with TLS certificates.
func (s *Server) HasSSL() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.SSLCertificate != "" && s.config.SSLCertificateKey != ""
}

// handleConnection reads one or more HTTP requests from a persistent connection.
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)

	for {
		// Set a read deadline: tight for the first request, longer for keep-alive idle.
		conn.SetReadDeadline(time.Now().Add(keepAliveTimeout))

		// Read the request line.
		requestLine, err := reader.ReadString('\n')
		if err != nil {
			return // Client closed or timed out — exit silently.
		}

		// Once we have a request line, enforce a stricter read timeout.
		conn.SetReadDeadline(time.Now().Add(readTimeout))

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

		// Parse headers until blank line.
		for {
			headerLine, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if headerLine == "\r\n" {
				break
			}

			headerParts := strings.SplitN(headerLine, ":", 2)
			if len(headerParts) != 2 {
				continue
			}

			key := strings.TrimSpace(headerParts[0])
			value := strings.TrimSpace(headerParts[1])
			req.Headers[key] = value
		}

		// Read the request body if Content-Length is specified.
		if cl, ok := req.Headers["Content-Length"]; ok {
			length, err := strconv.ParseInt(cl, 10, 64)
			if err == nil && length > 0 {
				req.Body = io.LimitReader(reader, length)
			}
		}

		// Set write deadline for the response.
		conn.SetWriteDeadline(time.Now().Add(writeTimeout))

		// Snapshot configuration, upstreams, and scheme under a read lock.
		s.mu.RLock()
		locationsSnapshot := s.config.Locations
		upstreamsSnapshot := s.upstreams
		s.mu.RUnlock()
		schemeSnapshot := s.scheme()

		// Route and dispatch.
		location := s.routeRequest(&req, locationsSnapshot)
		start := time.Now()
		var statusCode int
		hijacked := false

		switch {
		case location == nil:
			statusCode = 404
			sendErrorResponse(conn, statusCode)
		case location.Root != "":
			statusCode = serveStaticFile(conn, &req, location)
		case location.ProxyPass != "" && isWebSocketUpgrade(&req):
			statusCode = serveWebSocketProxy(conn, reader, &req, location, upstreamsSnapshot, schemeSnapshot)
			hijacked = true
		case location.ProxyPass != "":
			statusCode = serveReverseProxy(conn, &req, location, upstreamsSnapshot, schemeSnapshot)
		default:
			statusCode = 500
			sendErrorResponse(conn, statusCode)
		}

		log.Printf("%s %s %s %d %s",
			conn.RemoteAddr(), req.Method, req.URI, statusCode, time.Since(start))

		// A successful WebSocket upgrade owns the connection. The keep-alive
		// loop must not read more HTTP requests from it.
		if hijacked {
			return
		}

		// Drain any unread request body before the next request.
		if req.Body != nil {
			io.Copy(io.Discard, req.Body)
		}

		// Check if the client wants to close the connection.
		if strings.EqualFold(req.Headers["Connection"], "close") || req.Version == "HTTP/1.0" {
			return
		}
	}
}

// routeRequest finds the best location configuration for a given request.
// It uses a longest prefix matching algorithm.
func (s *Server) routeRequest(req *Request, locations []config.LocationConfig) *config.LocationConfig {
	var bestMatch *config.LocationConfig
	longestMatchLen := 0
	for i := range locations {
		if strings.HasPrefix(req.URI, locations[i].Path) {
			if len(locations[i].Path) > longestMatchLen {
				longestMatchLen = len(locations[i].Path)
				bestMatch = &locations[i]
			}
		}
	}
	return bestMatch
}
