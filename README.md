# Wisp

Wisp is a high-performance, minimalist web server and reverse proxy inspired by the architecture and efficiency of Nginx but built from first principles in Go. It's designed to be simple, understandable, and fast, leveraging Go's native concurrency with goroutines to handle a massive number of connections.

## MVP Features

The scope of the Minimum Viable Product (MVP) is focused on delivering the core functionalities expected from a modern web server.

- **High Concurrency**: Utilizes a **goroutine-per-connection** model to handle thousands of simultaneous clients efficiently.
- **Static File Serving**: Serves static assets like HTML, CSS, and JavaScript files from a specified root directory.
- **Reverse Proxy**: Forwards client requests to one or more backend services, acting as a single gateway for your applications.
- **Simple Configuration**: Uses a straightforward, human-readable configuration file to define server behavior, including virtual hosts.

## Configuration

Wisp is controlled by a simple configuration file. The MVP will support the following directives:

```
# Example wisp.conf

server {
    listen 8080;
    server_name example.com;

    location / {
        root /var/www/my-site;
    }

    location /api/ {
        proxy_pass http://127.0.0.1:3000;
    }
}
```

## Getting Started

To get the server running locally, follow these steps:

1.  **Clone the repository:**

    ```sh
    git clone https://github.com/Annany2002/wisp.git
    ```

2.  **Navigate to the project directory:**

    ```sh
    cd wisp
    ```

3.  **Run the server:**

    ```sh
    go run main.go
    ```

The server will then be listening on the port(s) defined in your configuration file.
