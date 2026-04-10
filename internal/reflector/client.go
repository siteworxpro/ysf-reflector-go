package reflector

import (
	"net"
	"sync"
	"time"
)


// client represents a connected YSF node/hotspot.
type client struct {
	addr     *net.UDPAddr
	callsign string
	lastSeen time.Time
}

// clientStore manages the set of active clients with safe concurrent access.
type clientStore struct {
	mu      sync.RWMutex
	clients map[string]*client // keyed by addr.String()
}

func newClientStore() *clientStore {
	return &clientStore{
		clients: make(map[string]*client),
	}
}

// upsert adds or refreshes a client entry, returning whether it is a new connection.
func (cs *clientStore) upsert(addr *net.UDPAddr, callsign string) (isNew bool) {
	key := addr.String()
	cs.mu.Lock()
	defer cs.mu.Unlock()

	c, exists := cs.clients[key]
	if !exists {
		cs.clients[key] = &client{addr: addr, callsign: callsign, lastSeen: time.Now()}
		return true
	}
	c.callsign = callsign
	c.lastSeen = time.Now()
	return false
}

// remove deletes a client by address string.
func (cs *clientStore) remove(key string) {
	cs.mu.Lock()
	delete(cs.clients, key)
	cs.mu.Unlock()
}

// snapshot returns a copy of all current client pointers (safe for iteration
// outside the lock).
func (cs *clientStore) snapshot() []*client {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	out := make([]*client, 0, len(cs.clients))
	for _, c := range cs.clients {
		out = append(out, c)
	}
	return out
}

// evictExpired removes clients that have not sent a poll within the timeout
// and returns their callsigns for logging.
func (cs *clientStore) evictExpired(timeout time.Duration) []string {
	deadline := time.Now().Add(-timeout)
	cs.mu.Lock()
	defer cs.mu.Unlock()

	var evicted []string
	for key, c := range cs.clients {
		if c.lastSeen.Before(deadline) {
			evicted = append(evicted, c.callsign)
			delete(cs.clients, key)
		}
	}
	return evicted
}

// count returns the number of connected clients.
func (cs *clientStore) count() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return len(cs.clients)
}
