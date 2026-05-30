package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Annany2002/wisp/internal/server"
)

func main() {
	configPath := flag.String("c", "wisp.conf", "path to configuration file")
	flag.Parse()

	sm := server.NewServerManager(*configPath)
	if err := sm.LoadAndApplyConfig(false); err != nil {
		log.Fatalf("Error loading configuration: %v", err)
	}

	// Handle OS signals for graceful shutdown and SIGHUP reload.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	go func() {
		for sig := range sigCh {
			switch sig {
			case syscall.SIGHUP:
				_ = sm.LoadAndApplyConfig(true)
			case syscall.SIGINT, syscall.SIGTERM:
				sm.Shutdown()
				return
			}
		}
	}()

	sm.Wg.Wait()
}
