package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Annany2002/wisp/internal/config"
	"github.com/Annany2002/wisp/internal/server"
)

func main() {
	configPath := flag.String("c", "wisp.conf", "path to configuration file")
	flag.Parse()

	cfg, err := config.Parse(*configPath)
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	srv := server.New(&cfg.Servers[0])

	// Handle OS signals for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		if err := srv.Shutdown(); err != nil {
			log.Printf("Error during shutdown: %v", err)
		}
	}()

	if err := srv.Start(); err != nil {
		log.Fatalf("Wisp server failed: %v", err)
	}
}
