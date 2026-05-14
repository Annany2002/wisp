package upstream

import (
	"log"
	"net"
	"net/http"
	"time"
)

const (
	// Defaults used when no health_check block is configured.
	defaultHealthCheckInterval = 10 * time.Second
	defaultHealthCheckTimeout  = 3 * time.Second
)

// StartHealthChecks launches a background goroutine that periodically
// probes each backend. If the upstream has an HTTP health_check config,
// probes are HTTP GETs against the configured path; otherwise they are
// plain TCP dials. Backends that respond pass; backends that fail are
// marked dead until they pass again.
//
// The returned stop function cancels the health check loop.
func (u *Upstream) StartHealthChecks() func() {
	done := make(chan struct{})

	interval := defaultHealthCheckInterval
	if u.HealthCheck != nil && u.HealthCheck.Interval > 0 {
		interval = u.HealthCheck.Interval
	}

	go func() {
		ticker := time.NewTicker(interval)
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
		alive := u.probe(b.Address)
		if alive && !b.Alive.Load() {
			b.Alive.Store(true)
			log.Printf("upstream %s: backend %s is UP", u.Name, b.Address)
		} else if !alive && b.Alive.Load() {
			b.Alive.Store(false)
			log.Printf("upstream %s: backend %s is DOWN", u.Name, b.Address)
		}
	}
}

// probe returns true when the backend is considered healthy. HTTP probing is
// used when health_check.path is configured; otherwise a TCP dial decides.
func (u *Upstream) probe(addr string) bool {
	timeout := defaultHealthCheckTimeout
	if u.HealthCheck != nil && u.HealthCheck.Timeout > 0 {
		timeout = u.HealthCheck.Timeout
	}

	if u.HealthCheck != nil && u.HealthCheck.Path != "" {
		return u.httpProbe(addr, timeout)
	}
	return u.tcpProbe(addr, timeout)
}

func (u *Upstream) tcpProbe(addr string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (u *Upstream) httpProbe(addr string, timeout time.Duration) bool {
	client := &http.Client{Timeout: timeout}
	url := "http://" + addr + u.HealthCheck.Path
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == u.HealthCheck.Expect
}
