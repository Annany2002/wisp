package upstream

import (
	"log"
	"net"
	"time"
)

const (
	// healthCheckInterval is how often backends are checked.
	healthCheckInterval = 10 * time.Second
	// healthCheckTimeout is the TCP dial timeout for a health probe.
	healthCheckTimeout = 3 * time.Second
)

// StartHealthChecks launches a background goroutine that periodically
// probes each backend's address with a TCP dial. Backends that respond
// are marked alive; those that fail are marked dead.
//
// The returned stop function cancels the health check loop.
func (u *Upstream) StartHealthChecks() func() {
	done := make(chan struct{})

	go func() {
		ticker := time.NewTicker(healthCheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				u.checkBackends()
			}
		}
	}()

	return func() { close(done) }
}

func (u *Upstream) checkBackends() {
	for _, b := range u.Backends {
		conn, err := net.DialTimeout("tcp", b.Address, healthCheckTimeout)
		if err != nil {
			if b.Alive.Load() {
				b.Alive.Store(false)
				log.Printf("upstream %s: backend %s is DOWN", u.Name, b.Address)
			}
			continue
		}
		conn.Close()
		if !b.Alive.Load() {
			b.Alive.Store(true)
			log.Printf("upstream %s: backend %s is UP", u.Name, b.Address)
		}
	}
}
