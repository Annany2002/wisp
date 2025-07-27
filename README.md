# Wisp

Wisp is a high-performance, minimalist web server and reverse proxy built from first principles in Go. It is inspired by the architecture and efficiency of Nginx but designed for simplicity and understandability. Wisp leverages Go's native goroutines to handle a massive number of connections with a minimal memory footprint.

## Features

The Minimum Viable Product (MVP) is feature-complete and includes:

- **High-Concurrency Core**: Uses a **goroutine-per-connection** model, allowing it to efficiently handle thousands of simultaneous clients.
- **Static File Serving**: Serves static assets like HTML, CSS, JavaScript, and images from a specified root directory. It automatically looks for `index.html` in directories and sets the correct `Content-Type` header.
- **Reverse Proxy**: Forwards client requests to backend services, acting as a single, simple gateway for your applications.
- **Configuration-Driven**: All behavior is controlled by a simple, human-readable `wisp.conf` file, parsed at startup.

## Configuration

Wisp's behavior is defined in a `wisp.conf` file. The configuration supports defining server properties and routing rules for different URL paths.

```
# Example wisp.conf

server {
    listen 8080;
    server_name wisp.test;

    # Serve a static website from this directory for requests
    # starting with '/'. This is the most general location.
    location / {
        root /var/www/my-site;
    }

    # Proxy all API requests to a backend application running on port 3000.
    # Because '/api/' is a longer prefix than '/', it will be matched first
    # for requests like '/api/users'.
    location /api/ {
        proxy_pass http://127.0.0.1:3000;
    }
}
```

## Getting Started

### Running with Air (Hot Reload)

[Air](https://github.com/air-verse/air) provides live reloading for Go applications. This project includes a pre-configured `.air.toml` file for convenience.

1.  Install Air (if not already installed):
    ```sh
    go install github.com/air-verse/air@latest
    ```
2.  From the project root, run:
    ```sh
    air
    ```
    This will automatically rebuild and restart the server on code changes. The configuration in `.air.toml` ensures the correct entrypoint is used..

### Using go binary

1.  **Clone the repository:**

    ```sh
    git clone https://github.com/Annany2002/wisp.git
    cd wisp
    ```

2.  **Build the binary:**

    ```sh
    go build -o wisp .
    ```

3.  **Run the server:**

    ```sh
    ./wisp
    ```

Wisp will start and listen on the port defined in your `wisp.conf` file.

## Future Enhancements

While the MVP is complete, the following improvements are planned for future versions:

- **Robust Testing**: Adding a comprehensive suite of unit and integration tests.
- **Virtual Hosting**: Supporting multiple `server { ... }` blocks to host different sites on the same instance.
- **Graceful Reloads**: The ability to reload the configuration without dropping active connections.
- **Enhanced Proxy**: Adding support for request bodies and WebSocket proxying.
- **TLS Support**: Enabling HTTPS by configuring SSL/TLS certificates.
