# Wisp

A minimalist web server and reverse proxy written in Go. Inspired by Nginx's architecture but designed for simplicity.

## Features

- **Static File Server**: Serve static files with MIME type detection
- **Reverse Proxy**: Route requests to backend services
- **TLS Support**: HTTPS with custom certificates
- **Nginx-style Config**: Familiar configuration syntax
- **Path-based Routing**: Longest prefix matching for request routing

## Quick Start

1. **Build and run**:

   ```sh
   go build -o wisp cmd/wisp/main.go
   ./wisp
   ```

2. **With Docker**:

   ```sh
   docker build -t wisp .
   docker run -v $(pwd)/wisp.conf:/root/wisp.conf -p 8080:8080 wisp
   ```

3. **With Docker Compose**:
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

- `/about.html` → serves from `/var/www/site/about.html`
- `/api/users` → proxies to `http://localhost:3000/users`
- `/api/v2/users` → proxies to `http://localhost:3001/users` (if configured)

## Development

**Hot reload with Air**:

```sh
go install github.com/air-verse/air@latest
air
```

**Test site included**: `wisp_test_site/` directory with sample files.

## Project Structure

```
wisp/
├── cmd/wisp/main.go          # Entry point
├── internal/
│   ├── config/              # Configuration parsing
│   └── server/              # HTTP server implementation
├── wisp_test_site/          # Test static files
├── wisp.conf               # Server configuration
└── README.md
```

## Configuration Reference

| Directive             | Description            | Example                             |
| --------------------- | ---------------------- | ----------------------------------- |
| `listen`              | Port to listen on      | `listen 8080;`                      |
| `server_name`         | Server identifier      | `server_name api.example.com;`      |
| `ssl_certificate`     | SSL certificate path   | `ssl_certificate ./cert.pem;`       |
| `ssl_certificate_key` | SSL key path           | `ssl_certificate_key ./key.pem;`    |
| `root`                | Static files directory | `root /var/www/site;`               |
| `proxy_pass`          | Backend service URL    | `proxy_pass http://localhost:3000;` |
