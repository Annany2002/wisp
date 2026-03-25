package main

import (
	"flag"
	"log"

	"github.com/Annany2002/wisp/internal/config"
	"github.com/Annany2002/wisp/internal/server"
)

func main() {
	configPath := flag.String("c", "wisp.conf", "path to configuration file")
	flag.Parse()

	// Load Configuration
	cfg, err := config.Parse(*configPath)
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	// Create a new server instance using the first server block.
	srv := server.New(&cfg.Servers[0])

	// Start the server
	if err := srv.Start(); err != nil {
		log.Fatalf("Wisp server failed: %v", err)
	}
}
