package server

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Annany2002/wisp/internal/config"
	"github.com/Annany2002/wisp/internal/upstream"
)

// compressibleTypes lists MIME type prefixes that benefit from gzip compression.
var compressibleTypes = []string{
	"text/",
	"application/json",
	"application/javascript",
	"application/xml",
	"application/xhtml+xml",
	"image/svg+xml",
}

// shouldCompress returns true if the content type is compressible and the
// client advertises gzip support via Accept-Encoding.
func shouldCompress(contentType string, req *Request) bool {
	ae := req.Headers["Accept-Encoding"]
	if !strings.Contains(ae, "gzip") {
		return false
	}
	for _, prefix := range compressibleTypes {
		if strings.HasPrefix(contentType, prefix) {
			return true
		}
	}
	return false
}

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

	// Decide whether to compress the response.
	if shouldCompress(contentType, req) {
		// Use chunked transfer encoding since compressed size is unknown.
		responseHeaders := fmt.Sprintf(
			"HTTP/1.1 200 OK\r\n"+
				"Content-Type: %s\r\n"+
				"Content-Encoding: gzip\r\n"+
				"Transfer-Encoding: chunked\r\n"+
				"Vary: Accept-Encoding\r\n"+
				"Date: %s\r\n"+
				"\r\n",
			contentType,
			time.Now().UTC().Format(time.RFC1123),
		)
		conn.Write([]byte(responseHeaders))

		cw := newChunkedWriter(conn)
		gz := gzip.NewWriter(cw)
		io.Copy(gz, file)
		gz.Close()
		_ = cw.Close()
	} else {
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

		_, err = io.Copy(conn, file)
		if err != nil {
			log.Printf("Failed to write file content: %v", err)
		}
	}
	return http.StatusOK
}

// resolveBackendURL resolves a proxy_pass value to an actual backend URL.
// If the host matches an upstream name, a backend is selected via the load balancer.
// Returns the resolved URL and an optional backend (for connection tracking).
func resolveBackendURL(proxyPass string, upstreams map[string]*upstream.Upstream) (*url.URL, *upstream.Backend, error) {
	parsed, err := url.Parse(proxyPass)
	if err != nil {
		return nil, nil, fmt.Errorf("malformed proxy_pass URL: %s", proxyPass)
	}

	// Check if the host refers to an upstream group.
	if upstreams != nil {
		if u, ok := upstreams[parsed.Hostname()]; ok {
			backend, err := u.Next()
			if err != nil {
				return nil, nil, err
			}
			// Replace the host with the selected backend address.
			resolved := *parsed
			resolved.Host = backend.Address
			return &resolved, backend, nil
		}
	}

	// Direct URL — no upstream resolution needed.
	return parsed, nil, nil
}

// hopByHopHeaders are stripped from forwarded requests per RFC 7230 §6.1.
// The WebSocket proxy path forwards Connection and Upgrade explicitly before
// consulting this map; all other proxy paths apply it verbatim.
var hopByHopHeaders = map[string]struct{}{
	"Connection":          {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

// proxyContext holds values used to expand nginx-style variables in
// proxy_set_header directives.
type proxyContext struct {
	clientIP   string
	scheme     string
	host       string
	xForwarded string // existing X-Forwarded-For chain (comma list)
}

// expandProxyVar replaces a single $var reference with its value, or returns
// the literal text if unknown.
func expandProxyVar(v string, ctx *proxyContext) string {
	if !strings.Contains(v, "$") {
		return v
	}
	replacer := strings.NewReplacer(
		"$remote_addr", ctx.clientIP,
		"$scheme", ctx.scheme,
		"$host", ctx.host,
		"$proxy_add_x_forwarded_for", appendForwardedFor(ctx.xForwarded, ctx.clientIP),
	)
	return replacer.Replace(v)
}

// appendForwardedFor appends clientIP to an existing comma-separated chain.
func appendForwardedFor(existing, clientIP string) string {
	if existing == "" {
		return clientIP
	}
	return existing + ", " + clientIP
}

// clientIPFromConn extracts the IP portion of conn.RemoteAddr() with no port.
func clientIPFromConn(conn net.Conn) string {
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return conn.RemoteAddr().String()
	}
	return host
}

// serveReverseProxy forwards a request to a backend service.
// Returns the HTTP status code sent to the client.
func serveReverseProxy(conn net.Conn, req *Request, loc *config.LocationConfig, upstreams map[string]*upstream.Upstream, scheme string) int {
	backendURL, backend, err := resolveBackendURL(loc.ProxyPass, upstreams)
	if err != nil {
		log.Printf("Backend resolution failed: %v", err)
		sendErrorResponse(conn, http.StatusBadGateway)
		return http.StatusBadGateway
	}

	// Track active connections for least_conn.
	if backend != nil {
		backend.ActiveConns.Add(1)
		defer backend.ActiveConns.Add(-1)
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

	clientIP := clientIPFromConn(conn)
	originHost := req.Headers["Host"]

	// Copy headers from the original request, skipping hop-by-hop.
	for key, value := range req.Headers {
		if _, hop := hopByHopHeaders[http.CanonicalHeaderKey(key)]; hop {
			continue
		}
		if strings.EqualFold(key, "Host") {
			continue // set via backendReq.Host below
		}
		backendReq.Header.Set(key, value)
	}

	// Default forwarded headers — overridable by user proxy_set_header.
	existingXFF := req.Headers["X-Forwarded-For"]
	backendReq.Header.Set("X-Forwarded-For", appendForwardedFor(existingXFF, clientIP))
	backendReq.Header.Set("X-Real-IP", clientIP)
	backendReq.Header.Set("X-Forwarded-Proto", scheme)
	if originHost != "" {
		backendReq.Header.Set("X-Forwarded-Host", originHost)
	}

	// Apply user-defined proxy_set_header (highest priority, overrides defaults).
	ctx := &proxyContext{
		clientIP:   clientIP,
		scheme:     scheme,
		host:       originHost,
		xForwarded: existingXFF,
	}
	for _, h := range loc.ProxySetHeaders {
		expanded := expandProxyVar(h.Value, ctx)
		if strings.EqualFold(h.Name, "Host") {
			backendReq.Host = expanded
			continue
		}
		if expanded == "" {
			backendReq.Header.Del(h.Name)
		} else {
			backendReq.Header.Set(h.Name, expanded)
		}
	}

	if backendReq.Host == "" {
		backendReq.Host = backendURL.Host
	}

	// Send the request to the backend.
	backendResp, err := http.DefaultClient.Do(backendReq)
	if err != nil {
		log.Printf("Failed to get response from backend %s: %v", backendURL.Host, err)
		// Mark backend as unhealthy on connection failure.
		if backend != nil {
			backend.Alive.Store(false)
		}
		sendErrorResponse(conn, http.StatusBadGateway)
		return http.StatusBadGateway
	}
	defer backendResp.Body.Close()

	// Write the backend's response back, dropping hop-by-hop headers.
	conn.Write([]byte(fmt.Sprintf("%s %s\r\n", backendResp.Proto, backendResp.Status)))
	for key, values := range backendResp.Header {
		if _, hop := hopByHopHeaders[http.CanonicalHeaderKey(key)]; hop {
			continue
		}
		for _, value := range values {
			conn.Write([]byte(fmt.Sprintf("%s: %s\r\n", key, value)))
		}
	}

	conn.Write([]byte("\r\n"))
	io.Copy(conn, backendResp.Body)

	return backendResp.StatusCode
}

// isWebSocketUpgrade returns true when the request asks for an HTTP/1.1
// protocol upgrade to WebSocket.
func isWebSocketUpgrade(req *Request) bool {
	conn := strings.ToLower(req.Headers["Connection"])
	upg := strings.ToLower(req.Headers["Upgrade"])
	return strings.Contains(conn, "upgrade") && upg == "websocket"
}

// serveWebSocketProxy upgrades the client connection and pipes bytes
// bidirectionally between the client and the selected backend.
// The caller must not touch clientConn or clientReader after this returns —
// the WebSocket frame stream has hijacked the connection.
func serveWebSocketProxy(clientConn net.Conn, clientReader *bufio.Reader, req *Request, loc *config.LocationConfig, upstreams map[string]*upstream.Upstream, scheme string) int {
	backendURL, backend, err := resolveBackendURL(loc.ProxyPass, upstreams)
	if err != nil {
		log.Printf("WS backend resolution failed: %v", err)
		sendErrorResponse(clientConn, http.StatusBadGateway)
		return http.StatusBadGateway
	}

	if backend != nil {
		backend.ActiveConns.Add(1)
		defer backend.ActiveConns.Add(-1)
	}

	backendConn, err := net.DialTimeout("tcp", backendURL.Host, 10*time.Second)
	if err != nil {
		log.Printf("WS backend dial failed %s: %v", backendURL.Host, err)
		if backend != nil {
			backend.Alive.Store(false)
		}
		sendErrorResponse(clientConn, http.StatusBadGateway)
		return http.StatusBadGateway
	}
	defer backendConn.Close()

	// Strip the location prefix from the URI before forwarding.
	newURI := strings.TrimPrefix(req.URI, loc.Path)
	if !strings.HasPrefix(newURI, "/") {
		newURI = "/" + newURI
	}

	// Reconstruct the request line + headers for the backend.
	// Connection and Upgrade headers must be forwarded verbatim; the rest of
	// the hop-by-hop list is stripped as usual.
	clientIP := clientIPFromConn(clientConn)
	originHost := req.Headers["Host"]

	var buf strings.Builder
	fmt.Fprintf(&buf, "%s %s %s\r\n", req.Method, newURI, req.Version)
	write := func(name, value string) {
		fmt.Fprintf(&buf, "%s: %s\r\n", name, value)
	}

	for k, v := range req.Headers {
		ck := http.CanonicalHeaderKey(k)
		if ck == "Host" {
			continue // emitted below
		}
		if ck == "Connection" || ck == "Upgrade" {
			write(ck, v)
			continue
		}
		if _, hop := hopByHopHeaders[ck]; hop {
			continue
		}
		write(ck, v)
	}
	if originHost != "" {
		write("Host", originHost)
	} else {
		write("Host", backendURL.Host)
	}

	existingXFF := req.Headers["X-Forwarded-For"]
	write("X-Forwarded-For", appendForwardedFor(existingXFF, clientIP))
	write("X-Real-IP", clientIP)
	write("X-Forwarded-Proto", scheme)
	if originHost != "" {
		write("X-Forwarded-Host", originHost)
	}
	buf.WriteString("\r\n")

	if _, err := backendConn.Write([]byte(buf.String())); err != nil {
		log.Printf("WS handshake write to backend failed: %v", err)
		return http.StatusBadGateway
	}

	// Read the backend's response status line + headers, forward verbatim.
	backendReader := bufio.NewReader(backendConn)
	statusLine, err := backendReader.ReadString('\n')
	if err != nil {
		log.Printf("WS backend status read failed: %v", err)
		sendErrorResponse(clientConn, http.StatusBadGateway)
		return http.StatusBadGateway
	}

	status := http.StatusBadGateway
	if parts := strings.SplitN(strings.TrimSpace(statusLine), " ", 3); len(parts) >= 2 {
		if code, perr := strconv.Atoi(parts[1]); perr == nil {
			status = code
		}
	}

	if _, err := clientConn.Write([]byte(statusLine)); err != nil {
		return status
	}
	for {
		line, err := backendReader.ReadString('\n')
		if err != nil {
			return status
		}
		if _, err := clientConn.Write([]byte(line)); err != nil {
			return status
		}
		if line == "\r\n" {
			break
		}
	}

	// Non-101 responses: backend declined the upgrade. Surface the status
	// to the access log but don't splice; the connection will close.
	if status != http.StatusSwitchingProtocols {
		return status
	}

	// 101 Switching Protocols — splice bidirectionally with no deadlines.
	// Any bytes already buffered in either reader must be flushed first or
	// the first WS frame may be lost.
	_ = clientConn.SetReadDeadline(time.Time{})
	_ = clientConn.SetWriteDeadline(time.Time{})

	errCh := make(chan error, 2)
	go func() {
		if n := clientReader.Buffered(); n > 0 {
			if pre, perr := clientReader.Peek(n); perr == nil {
				if _, werr := backendConn.Write(pre); werr != nil {
					errCh <- werr
					return
				}
				_, _ = clientReader.Discard(n)
			}
		}
		_, err := io.Copy(backendConn, clientConn)
		errCh <- err
	}()
	go func() {
		if n := backendReader.Buffered(); n > 0 {
			if pre, perr := backendReader.Peek(n); perr == nil {
				if _, werr := clientConn.Write(pre); werr != nil {
					errCh <- werr
					return
				}
				_, _ = backendReader.Discard(n)
			}
		}
		_, err := io.Copy(clientConn, backendConn)
		errCh <- err
	}()

	<-errCh
	return status
}

// chunkedWriter implements io.WriteCloser for HTTP chunked transfer encoding.
type chunkedWriter struct {
	conn net.Conn
}

func newChunkedWriter(conn net.Conn) *chunkedWriter {
	return &chunkedWriter{conn: conn}
}

func (cw *chunkedWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	// Write chunk size in hex, then CRLF, then data, then CRLF.
	header := fmt.Sprintf("%x\r\n", len(p))
	if _, err := cw.conn.Write([]byte(header)); err != nil {
		return 0, err
	}
	n, err := cw.conn.Write(p)
	if err != nil {
		return n, err
	}
	_, err = cw.conn.Write([]byte("\r\n"))
	return n, err
}

// Close writes the terminating zero-length chunk.
func (cw *chunkedWriter) Close() error {
	_, err := cw.conn.Write([]byte("0\r\n\r\n"))
	return err
}
