package reflector

import (
	"net"
	"testing"
	"time"
)

func makeAddr(t *testing.T, s string) *net.UDPAddr {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", s)
	if err != nil {
		t.Fatalf("ResolveUDPAddr(%q): %v", s, err)
	}
	return addr
}

// ---------- upsert / count ----------

func TestUpsert_NewClient(t *testing.T) {
	cs := newClientStore()
	addr := makeAddr(t, "192.0.2.1:42000")
	isNew := cs.upsert(addr, "W1AW")
	if !isNew {
		t.Fatal("expected isNew=true for first insert")
	}
	if cs.count() != 1 {
		t.Fatalf("count: got %d, want 1", cs.count())
	}
}

func TestUpsert_ExistingClient(t *testing.T) {
	cs := newClientStore()
	addr := makeAddr(t, "192.0.2.1:42000")
	cs.upsert(addr, "W1AW")
	isNew := cs.upsert(addr, "W1AW")
	if isNew {
		t.Fatal("expected isNew=false on second upsert for same address")
	}
	if cs.count() != 1 {
		t.Fatalf("count: got %d, want 1", cs.count())
	}
}

func TestUpsert_UpdatesCallsign(t *testing.T) {
	cs := newClientStore()
	addr := makeAddr(t, "192.0.2.1:42000")
	cs.upsert(addr, "OLDCALL")
	cs.upsert(addr, "NEWCALL")

	clients := cs.snapshot()
	if len(clients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(clients))
	}
	if clients[0].callsign != "NEWCALL" {
		t.Fatalf("callsign: got %q, want %q", clients[0].callsign, "NEWCALL")
	}
}

func TestUpsert_MultipleClients(t *testing.T) {
	cs := newClientStore()
	cs.upsert(makeAddr(t, "192.0.2.1:42000"), "W1AW")
	cs.upsert(makeAddr(t, "192.0.2.2:42000"), "KD9ABC")
	cs.upsert(makeAddr(t, "192.0.2.3:42000"), "VK2XYZ")

	if cs.count() != 3 {
		t.Fatalf("count: got %d, want 3", cs.count())
	}
}

// ---------- remove ----------

func TestRemove_Existing(t *testing.T) {
	cs := newClientStore()
	addr := makeAddr(t, "192.0.2.1:42000")
	cs.upsert(addr, "W1AW")
	cs.remove(addr.String())
	if cs.count() != 0 {
		t.Fatalf("count after remove: got %d, want 0", cs.count())
	}
}

func TestRemove_NonExistent(t *testing.T) {
	cs := newClientStore()
	// Should not panic on a key that doesn't exist.
	cs.remove("192.0.2.99:42000")
	if cs.count() != 0 {
		t.Fatalf("count: got %d, want 0", cs.count())
	}
}

// ---------- snapshot ----------

func TestSnapshot_Empty(t *testing.T) {
	cs := newClientStore()
	snap := cs.snapshot()
	if len(snap) != 0 {
		t.Fatalf("expected empty snapshot, got %d entries", len(snap))
	}
}

func TestSnapshot_ContainsAll(t *testing.T) {
	cs := newClientStore()
	cs.upsert(makeAddr(t, "192.0.2.1:42000"), "W1AW")
	cs.upsert(makeAddr(t, "192.0.2.2:42000"), "KD9ABC")

	snap := cs.snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 entries in snapshot, got %d", len(snap))
	}
}

func TestSnapshot_IsIndependent(t *testing.T) {
	cs := newClientStore()
	addr := makeAddr(t, "192.0.2.1:42000")
	cs.upsert(addr, "W1AW")

	snap := cs.snapshot()
	// Mutating the snapshot slice should not affect the store.
	snap[0] = nil
	if cs.count() != 1 {
		t.Fatal("modifying snapshot affected the store")
	}
}

// ---------- evictExpired ----------

func TestEvictExpired_None(t *testing.T) {
	cs := newClientStore()
	cs.upsert(makeAddr(t, "192.0.2.1:42000"), "W1AW")

	evicted := cs.evictExpired(60 * time.Second)
	if len(evicted) != 0 {
		t.Fatalf("expected 0 evictions, got %d", len(evicted))
	}
	if cs.count() != 1 {
		t.Fatalf("count after evict: got %d, want 1", cs.count())
	}
}

func TestEvictExpired_All(t *testing.T) {
	cs := newClientStore()
	cs.upsert(makeAddr(t, "192.0.2.1:42000"), "W1AW")
	cs.upsert(makeAddr(t, "192.0.2.2:42000"), "KD9ABC")

	// A timeout of 0 means everything is already past the deadline.
	evicted := cs.evictExpired(0)
	if len(evicted) != 2 {
		t.Fatalf("expected 2 evictions, got %d", len(evicted))
	}
	if cs.count() != 0 {
		t.Fatalf("count after evict: got %d, want 0", cs.count())
	}
}

func TestEvictExpired_Partial(t *testing.T) {
	cs := newClientStore()

	// Add a client and immediately back-date its lastSeen.
	staleAddr := makeAddr(t, "192.0.2.1:42000")
	cs.upsert(staleAddr, "STALE")
	cs.mu.Lock()
	cs.clients[staleAddr.String()].lastSeen = time.Now().Add(-10 * time.Minute)
	cs.mu.Unlock()

	// Add a fresh client.
	cs.upsert(makeAddr(t, "192.0.2.2:42000"), "FRESH")

	evicted := cs.evictExpired(5 * time.Minute)
	if len(evicted) != 1 || evicted[0] != "STALE" {
		t.Fatalf("expected [STALE] evicted, got %v", evicted)
	}
	if cs.count() != 1 {
		t.Fatalf("count after evict: got %d, want 1", cs.count())
	}
}

func TestEvictExpired_ReturnsCallsigns(t *testing.T) {
	cs := newClientStore()
	addr := makeAddr(t, "192.0.2.1:42000")
	cs.upsert(addr, "MYCALL")

	evicted := cs.evictExpired(0)
	if len(evicted) != 1 || evicted[0] != "MYCALL" {
		t.Fatalf("expected [MYCALL], got %v", evicted)
	}
}
