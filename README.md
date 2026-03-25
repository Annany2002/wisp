# Wisp

A minimalist web server and reverse proxy written in Go. Inspired by Nginx's architecture but designed for simplicity.

## Features

- **Static File Server**: Serve static files with MIME type detection
- **Reverse Proxy**: Route requests to backend services with full body forwarding
- **Gzip Compression**: Automatic compression for text-based responses (HTML, CSS, JS, JSON, SVG, XML)
- **TLS Support**: HTTPS with custom certificates
- **Nginx-style Config**: Familiar configuration syntax with multiple server blocks
- **Path-based Routing**: Longest prefix matching for request routing
- **HTTP Keep-Alive**: Persistent connections for improved performance
- **Graceful Shutdown**: Clean shutdown on SIGINT/SIGTERM with connection draining
- **Access Logging**: Structured request logs with client IP, method, URI, status, and duration
- **Security**: Path traversal protection and connection timeouts

## Quick Start

1. **Build and run**:

   ```sh
   go build -o wisp cmd/wisp/main.go
   ./wisp
   ```

2. **Custom config path**:

   ```sh
   ./wisp -c /path/to/wisp.conf
   ```

3. **With Docker**:

   ```sh
   docker build -t wisp .
   docker run -v $(pwd)/wisp.conf:/root/wisp.conf -p 8080:8080 wisp
   ```

4. **With Docker Compose**:
   ```sh
   docker compose up
   ```

## Configuration

Wisp uses `wisp.conf` with Nginx-style syntax:

```nginx
server {
    listen 8080;
    server_name example.com;

    # Serve static files
    location / {
        root /var/www/site;
    }

    # Proxy to backend
    location /api/ {
        proxy_pass http://localhost:3000;
    }
}
```

### Multiple Server Blocks

Run multiple servers on different ports from a single config:

```nginx
server {
    listen 8080;
    server_name web.example.com;

    location / {
        root /var/www/site;
    }
}

server {
    listen 8443;
    server_name api.example.com;

    ssl_certificate     ./cert.pem;
    ssl_certificate_key ./key.pem;

    location /api/ {
        proxy_pass http://localhost:3000;
    }
}
```

### HTTPS Setup

```nginx
server {
    listen 8443;
    server_name localhost;

    ssl_certificate     ./cert.pem;
    ssl_certificate_key ./key.pem;

    location / {
        root ./wisp_test_site;
    }
}
```

### Routing

Wisp matches requests to the most specific location:

- `/about.html` -> serves from `/var/www/site/about.html`
- `/api/users` -> proxies to `http://localhost:3000/users`
- `/api/v2/users` -> proxies to `http://localhost:3001/users` (if configured)

## Development

**Hot reload with Air**:

```sh
go install github.com/air-verse/air@latest
air
```

**Run tests**:

```sh
go test ./...
```

**Test site included**: `wisp_test_site/` directory with sample files.

## Project Structure

```
wisp/
├── cmd/wisp/main.go          # Entry point with CLI flags and signal handling
├── internal/
│   ├── config/               # Nginx-style configuration parser
│   └── server/               # HTTP server, router, and handlers
├── wisp_test_site/           # Test static files
├── wisp.conf                 # Server configuration
├── Dockerfile                # Multi-stage Docker build
└── docker-compose.yml
```

## Configuration Reference

| Directive             | Context  | Description            | Example                             |
| --------------------- | -------- | ---------------------- | ----------------------------------- |
| `listen`              | server   | Port to listen on      | `listen 8080;`                      |
| `server_name`         | server   | Server identifier      | `server_name api.example.com;`      |
| `ssl_certificate`     | server   | SSL certificate path   | `ssl_certificate ./cert.pem;`       |
| `ssl_certificate_key` | server   | SSL key path           | `ssl_certificate_key ./key.pem;`    |
| `root`                | location | Static files directory | `root /var/www/site;`               |
| `proxy_pass`          | location | Backend service URL    | `proxy_pass http://localhost:3000;` |
