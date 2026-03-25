package upstream

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/Annany2002/wisp/internal/config"
)

// Backend represents a single backend server with health and connection tracking.
type Backend struct {
	Address     string
	Weight      int
	Alive       atomic.Bool
	ActiveConns atomic.Int64
}

// Upstream is a group of backends with a load balancing strategy.
type Upstream struct {
	Name     string
	Method   string
	Backends []*Backend

	// Round-robin state.
	mu      sync.Mutex
	rrIndex int
}

// New creates an Upstream from a parsed config.
func New(cfg *config.UpstreamConfig) *Upstream {
	u := &Upstream{
		Name:     cfg.Name,
		Method:   cfg.Method,
		Backends: make([]*Backend, len(cfg.Backends)),
	}
	for i, b := range cfg.Backends {
		be := &Backend{
			Address: b.Address,
			Weight:  b.Weight,
		}
		be.Alive.Store(true)
		u.Backends[i] = be
	}
	return u
}

// Next returns the next available backend based on the configured method.
// Returns an error if no healthy backends are available.
func (u *Upstream) Next() (*Backend, error) {
	switch u.Method {
	case "least_conn":
		return u.leastConn()
	default:
		return u.roundRobin()
	}
}

// roundRobin selects the next backend using weighted round-robin.
func (u *Upstream) roundRobin() (*Backend, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	total := len(u.Backends)
	for i := 0; i < total; i++ {
		idx := (u.rrIndex + i) % total
		b := u.Backends[idx]
		if b.Alive.Load() {
			u.rrIndex = (idx + 1) % total
			return b, nil
		}
	}
	return nil, fmt.Errorf("no healthy backends in upstream %s", u.Name)
}

// leastConn selects the backend with the fewest active connections,
// weighted by the backend's weight (effective load = conns / weight).
func (u *Upstream) leastConn() (*Backend, error) {
	var best *Backend
	bestLoad := float64(-1)

	for _, b := range u.Backends {
		if !b.Alive.Load() {
			continue
		}
		// Lower effective load = better candidate.
		load := float64(b.ActiveConns.Load()) / float64(b.Weight)
		if best == nil || load < bestLoad {
			best = b
			bestLoad = load
		}
	}

	if best == nil {
		return nil, fmt.Errorf("no healthy backends in upstream %s", u.Name)
	}
	return best, nil
}
