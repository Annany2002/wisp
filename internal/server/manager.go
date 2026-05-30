package server

import (
	"log"
	"sync"

	"github.com/Annany2002/wisp/internal/config"
	"github.com/Annany2002/wisp/internal/upstream"
)

// ServerManager coordinates the life-cycle of Wisp server listeners and health check loops.
type ServerManager struct {
	configPath string
	mu         sync.Mutex
	servers    map[int]*Server
	upstreams  map[string]*upstream.Upstream
	stops      []func()
	Wg         sync.WaitGroup
}

// NewServerManager creates an empty ServerManager.
func NewServerManager(configPath string) *ServerManager {
	return &ServerManager{
		configPath: configPath,
		servers:    make(map[int]*Server),
		upstreams:  make(map[string]*upstream.Upstream),
	}
}

// LoadAndApplyConfig reads the configuration from disk, updates existing listeners
// dynamically, starts new listeners, shuts down obsolete listeners, and updates upstreams.
func (sm *ServerManager) LoadAndApplyConfig(isReload bool) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if isReload {
		log.Printf("Reloading configuration from %s...", sm.configPath)
	}

	cfg, err := config.Parse(sm.configPath)
	if err != nil {
		if isReload {
			log.Printf("Error reloading configuration (keeping active config): %v", err)
			return err
		}
		log.Fatalf("Error loading configuration: %v", err)
	}

	// 1. Build the new upstreams, preserving state from the old upstreams.
	newUpstreams := make(map[string]*upstream.Upstream, len(cfg.Upstreams))
	var newStops []func()
	for i := range cfg.Upstreams {
		cfgUpstream := &cfg.Upstreams[i]
		var oldUpstream *upstream.Upstream
		if sm.upstreams != nil {
			oldUpstream = sm.upstreams[cfgUpstream.Name]
		}
		u := upstream.NewWithState(cfgUpstream, oldUpstream)
		newUpstreams[u.Name] = u
		stop := u.StartHealthChecks()
		newStops = append(newStops, stop)
		log.Printf("Upstream '%s' configured with %d backends (%s)",
			u.Name, len(u.Backends), cfgUpstream.Method)
	}

	// Stop all old health check tickers.
	for _, stop := range sm.stops {
		if stop != nil {
			stop()
		}
	}
	sm.upstreams = newUpstreams
	sm.stops = newStops

	// 2. Reconcile server blocks.
	newPorts := make(map[int]*config.ServerConfig)
	for i := range cfg.Servers {
		newPorts[cfg.Servers[i].Listen] = &cfg.Servers[i]
	}

	// Shutdown port listeners that are no longer configured.
	for port, srv := range sm.servers {
		if _, exists := newPorts[port]; !exists {
			log.Printf("Port %d is no longer configured. Shutting down server...", port)
			if err := srv.Shutdown(); err != nil {
				log.Printf("Error shutting down server on port %d: %v", port, err)
			}
			delete(sm.servers, port)
		}
	}

	// Start new listeners or hot-reload existing ones.
	for port, srvCfg := range newPorts {
		existingSrv, exists := sm.servers[port]
		if exists {
			oldSSL := existingSrv.HasSSL()
			newSSL := srvCfg.SSLCertificate != "" && srvCfg.SSLCertificateKey != ""
			if oldSSL != newSSL {
				log.Printf("SSL configuration status changed for port %d. Restarting server listener...", port)
				if err := existingSrv.Shutdown(); err != nil {
					log.Printf("Error shutting down old server on port %d: %v", port, err)
				}
				srv := New(srvCfg, newUpstreams)
				sm.servers[port] = srv
				sm.Wg.Add(1)
				go func(s *Server) {
					defer sm.Wg.Done()
					if err := s.Start(); err != nil {
						log.Printf("Server failed: %v", err)
					}
				}(srv)
			} else {
				log.Printf("Updating configuration dynamically for port %d...", port)
				if err := existingSrv.UpdateConfig(srvCfg, newUpstreams); err != nil {
					log.Printf("Error dynamically updating config for port %d: %v", port, err)
				}
			}
		} else {
			log.Printf("Starting new server block on port %d...", port)
			srv := New(srvCfg, newUpstreams)
			sm.servers[port] = srv
			sm.Wg.Add(1)
			go func(s *Server) {
				defer sm.Wg.Done()
				if err := s.Start(); err != nil {
					log.Printf("Server failed: %v", err)
				}
			}(srv)
		}
	}

	return nil
}

// Shutdown gracefully shuts down all port listeners and stops all active health checks.
func (sm *ServerManager) Shutdown() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	log.Println("Shutting down Wisp servers...")
	for _, stop := range sm.stops {
		if stop != nil {
			stop()
		}
	}
	for _, srv := range sm.servers {
		if err := srv.Shutdown(); err != nil {
			log.Printf("Error during shutdown: %v", err)
		}
	}
}
