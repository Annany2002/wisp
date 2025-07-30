# Wisp

Wisp is a high-performance, minimalist web server and reverse proxy built from first principles in Go. It is inspired by the architecture and efficiency of Nginx but designed for simplicity and understandability. Wisp leverages Go's native goroutines to handle a massive number of connections with a minimal memory footprint.

## Features

The server implementation includes:

- **High-Concurrency Core**: Uses a **goroutine-per-connection** model, allowing it to efficiently handle thousands of simultaneous clients with minimal memory footprint.
- **Static File Serving**:
  - Serves static assets like HTML, CSS, JavaScript, and images from configured root directories
  - Automatic `index.html` detection in directories
  - Content-Type detection based on file extensions
  - Proper handling of file sizes and streaming for efficient delivery
- **Reverse Proxy**:
  - Forwards client requests to configured backend services
  - Preserves original request headers
  - Proper handling of backend responses and streaming
  - Support for path-based routing to different backends
- **Configuration-Driven**:
  - Simple, human-readable `wisp.conf` file controls all behavior
  - Support for multiple location blocks with different configurations
  - Flexible routing based on URL paths

## Configuration

Wisp's behavior is defined in a `wisp.conf` file. The configuration follows a simple, Nginx-like syntax and supports defining server properties and routing rules for different URL paths.

### Basic Structure

```
server {
    # Server-wide settings
    listen 8080;           # Port to listen on
    server_name wisp;      # Server name identifier

    # Location blocks for routing
    location / {
        root ./path/to/static/files;  # Serve static files
    }

    location /api/ {
        proxy_pass http://localhost:3000;  # Reverse proxy
    }
}
```

### Location Matching

Location blocks are matched based on the longest prefix match. For example, with the following configuration:

```
location / {
    root ./public;
}

location /api/ {
    proxy_pass http://localhost:3000;
}

location /api/v2/ {
    proxy_pass http://localhost:3001;
}
```

- A request to `/index.html` matches the `/` location and serves from `./public`
- A request to `/api/users` matches `/api/` and proxies to port 3000
- A request to `/api/v2/users` matches `/api/v2/` and proxies to port 3001

### Supported Directives

Server block directives:

- `listen`: Port number to listen on
- `server_name`: Server identifier

Location block directives:

- `root`: Path to serve static files from
- `proxy_pass`: URL to proxy requests to

## How to run

### Development Setup

For development, you can use the included test site and configuration:

1. The repository includes a `wisp_test_site` directory with a sample static website
2. The default `wisp.conf` is configured to serve this test site and proxy `/api/` requests
3. You can modify the test site or configuration to test different scenarios

#### Using Air (Hot Reload)

[Air](https://github.com/air-verse/air) provides live reloading for Go applications during development.

1. Install Air (if not already installed):
   ```sh
   go install github.com/air-verse/air@latest
   ```
2. From the project root, run:
   ```sh
   air
   ```
   This will automatically rebuild and restart the server when code changes are detected.

### Using docker-compose

1. To start the application using Docker Compose:

   ```sh
   docker compose up
   ```

   This will start the wisp-server on port **8080**.

### Using docker

1. Build the image
   ```sh
   docker build -t wisp:tag-name .
   ```
2. Run the image

   ```sh
   docker run -v $(pwd)/wisp.conf:/root/wisp.conf -p 8080:8080 wisp
   ```

   The server will start on port **8080**.

### Standard Execution (Local)

1.  **Clone the repository:**

    ```sh
    git clone https://github.com/Annany2002/wisp.git
    cd wisp
    ```

2.  **Build the binary:**

    ```sh
    go build -o wisp cmd/wisp/main.go
    ```

3.  **Run the server:**

    ```sh
    ./wisp
    ```

Wisp will start and listen on the port defined in your `wisp.conf` file.

## Project Structure

```
.
├── cmd/
│   └── wisp/
│       └── main.go          # Entry point
├── internal/
│   ├── config/             # Configuration handling
│   │   ├── config.go
│   │   └── config_test.go
│   └── server/             # Core server implementation
│       ├── handler.go      # Request handlers
│       ├── server.go       # Server logic
│       └── server_test.go
├── docker-compose.yml      # Docker compose configuration
├── Dockerfile             # Docker build configuration
├── go.mod                 # Go module definition
├── README.md             # Documentation
└── wisp.conf            # Server configuration
```

## Future Enhancements

The following improvements are planned for future versions:

- **HTTP/2 Support**: Adding support for HTTP/2 protocol
- **Virtual Hosting**: Supporting multiple `server { ... }` blocks to host different sites on the same instance
- **Graceful Reloads**: The ability to reload the configuration without dropping active connections
- **Enhanced Proxy Features**:
  - WebSocket proxying
  - Request body handling
  - Load balancing
  - Health checks
- **Security Features**:
  - TLS/HTTPS support
  - Basic authentication
  - Rate limiting
  - IP filtering
- **Caching Layer**: Adding support for response caching and cache control
- **Logging Enhancements**:
  - Configurable log formats
  - Access and error logs
  - Log rotation
