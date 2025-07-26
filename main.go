package main

import (
	"log"
	"net"
)

// handleConnection manages a single client connection.
func handleConnection(conn net.Conn) {
	// Ensure the connection is closed when the function completes.
	defer conn.Close()
	log.Printf("Handling connection from %s", conn.RemoteAddr())
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
