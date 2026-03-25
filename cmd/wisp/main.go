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
	"github.com/Annany2002/wisp/internal/upstream"
)

func main() {
	configPath := flag.String("c", "wisp.conf", "path to configuration file")
	flag.Parse()

	cfg, err := config.Parse(*configPath)
	if err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	// Build the upstream map from config and start health checks.
	upstreams := make(map[string]*upstream.Upstream, len(cfg.Upstreams))
	var healthCheckStops []func()
	for i := range cfg.Upstreams {
		u := upstream.New(&cfg.Upstreams[i])
		upstreams[u.Name] = u
		stop := u.StartHealthChecks()
		healthCheckStops = append(healthCheckStops, stop)
		log.Printf("Upstream '%s' configured with %d backends (%s)",
			u.Name, len(u.Backends), cfg.Upstreams[i].Method)
	}

	// Start all server blocks concurrently.
	servers := make([]*server.Server, len(cfg.Servers))
	var wg sync.WaitGroup

	for i := range cfg.Servers {
		servers[i] = server.New(&cfg.Servers[i], upstreams)
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
		// Stop health checks.
		for _, stop := range healthCheckStops {
			stop()
		}
		// Shutdown all servers.
		for _, srv := range servers {
			if err := srv.Shutdown(); err != nil {
				log.Printf("Error during shutdown: %v", err)
			}
		}
	}()

	wg.Wait()
}
