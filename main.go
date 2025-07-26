package main

import (
	"log"
	"net"
)

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

		log.Printf("Accepted connection from %s", conn.RemoteAddr())
		conn.Close()
	}
}
