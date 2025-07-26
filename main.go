package main

import (
	"log"
	"net"
)

func main() {
	// Define the address to listen on.
	// We hardcode it for now; configuration parsing will come later.
	const address = ":8080"

	// Create a TCP listener on the specified address.
	// net.Listen returns a listener object that waits for incoming connections.
	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
	// We defer the closing of the listener to ensure it's cleaned up
	// when the main function exits.
	defer listener.Close()

	log.Printf("Wisp server listening on %s", address)

	// The main application loop. It runs forever, waiting for connections.
	for {
		// listener.Accept() is a blocking call. It waits until a client
		// connects and then returns a net.Conn object for that connection.
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			// Continue to the next iteration to not crash the server on a bad connection.
			continue
		}

		// For now, we just acknowledge the connection and close it immediately.
		// Handling the connection will be the next step.
		log.Printf("Accepted connection from %s", conn.RemoteAddr())
		conn.Close()
	}
}
