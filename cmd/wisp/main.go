package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
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

	// Start all server blocks concurrently.
	servers := make([]*server.Server, len(cfg.Servers))
	var wg sync.WaitGroup

	for i := range cfg.Servers {
		servers[i] = server.New(&cfg.Servers[i])
		wg.Add(1)
		go func(srv *server.Server) {
			defer wg.Done()
			if err := srv.Start(); err != nil {
				log.Printf("Server failed: %v", err)
			}
		}(servers[i])
	}

	// Handle OS signals for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		for _, srv := range servers {
			if err := srv.Shutdown(); err != nil {
				log.Printf("Error during shutdown: %v", err)
			}
		}
	}()

	wg.Wait()
}
