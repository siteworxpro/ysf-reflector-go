package reflector

import (
	"bytes"
	"testing"

	"github.com/siteworxpro/ysf-reflector-go/internal/config"
)

// ---------- trimCallsign ----------

func TestTrimCallsign_SpacePadded(t *testing.T) {
	b := []byte("W1AW      ")
	got := trimCallsign(b)
	if got != "W1AW" {
		t.Fatalf("got %q, want %q", got, "W1AW")
	}
}

func TestTrimCallsign_NulPadded(t *testing.T) {
	b := []byte{'W', '1', 'A', 'W', 0, 0, 0, 0, 0, 0}
	got := trimCallsign(b)
	if got != "W1AW" {
		t.Fatalf("got %q, want %q", got, "W1AW")
	}
}

func TestTrimCallsign_Full(t *testing.T) {
	b := []byte("1234567890")
	got := trimCallsign(b)
	if got != "1234567890" {
		t.Fatalf("got %q, want %q", got, "1234567890")
	}
}

func TestTrimCallsign_Empty(t *testing.T) {
	b := []byte("          ")
	got := trimCallsign(b)
	if got != "" {
		t.Fatalf("got %q, want empty string", got)
	}
}

// ---------- parsePollCallsign ----------

func makePollPacket(callsign string) []byte {
	pkt := make([]byte, PollSize)
	copy(pkt[0:4], magicPoll)
	cs := make([]byte, 10)
	for i := range cs {
		cs[i] = ' '
	}
	copy(cs, callsign)
	copy(pkt[4:14], cs)
	return pkt
}

func TestParsePollCallsign(t *testing.T) {
	pkt := makePollPacket("KD9ABC")
	got := parsePollCallsign(pkt)
	if got != "KD9ABC" {
		t.Fatalf("got %q, want %q", got, "KD9ABC")
	}
}

// ---------- parseDataSrcCallsign ----------

func makeDataPacket(srcCallsign string) []byte {
	pkt := make([]byte, DataSize)
	copy(pkt[0:4], magicData)
	src := make([]byte, 10)
	for i := range src {
		src[i] = ' '
	}
	copy(src, srcCallsign)
	copy(pkt[4:14], src)
	return pkt
}

func TestParseDataSrcCallsign(t *testing.T) {
	pkt := makeDataPacket("N0CALL")
	got := parseDataSrcCallsign(pkt)
	if got != "N0CALL" {
		t.Fatalf("got %q, want %q", got, "N0CALL")
	}
}

// ---------- parseUnlinkCallsign ----------

func makeUnlinkPacket(callsign string) []byte {
	pkt := make([]byte, UnlinkSize)
	copy(pkt[0:4], magicUnlink)
	cs := make([]byte, 10)
	for i := range cs {
		cs[i] = ' '
	}
	copy(cs, callsign)
	copy(pkt[4:14], cs)
	return pkt
}

func TestParseUnlinkCallsign(t *testing.T) {
	pkt := makeUnlinkPacket("VK2XYZ")
	got := parseUnlinkCallsign(pkt)
	if got != "VK2XYZ" {
		t.Fatalf("got %q, want %q", got, "VK2XYZ")
	}
}

// ---------- buildPollReply ----------

func TestBuildPollReply_Length(t *testing.T) {
	cs := []byte("W1AW      ")
	reply := buildPollReply(cs)
	if len(reply) != PollSize {
		t.Fatalf("expected length %d, got %d", PollSize, len(reply))
	}
}

func TestBuildPollReply_Magic(t *testing.T) {
	cs := []byte("W1AW      ")
	reply := buildPollReply(cs)
	if string(reply[0:4]) != magicPoll {
		t.Fatalf("magic: got %q, want %q", reply[0:4], magicPoll)
	}
}

func TestBuildPollReply_Callsign(t *testing.T) {
	cs := []byte("W1AW      ")
	reply := buildPollReply(cs)
	if !bytes.Equal(reply[4:14], cs) {
		t.Fatalf("callsign: got %q, want %q", reply[4:14], cs)
	}
}

// ---------- buildStatusReply ----------

func newTestConfig() *config.Config {
	return &config.Config{
		Callsign:    "W1AW",
		ID:          12345,
		Name:        "TestNet",
		Description: "A test net",
		Port:        42000,
		HTTPPort:    8080,
		Timeout:     240,
	}
}

func TestBuildStatusReply_Length(t *testing.T) {
	cfg := newTestConfig()
	reply := buildStatusReply(cfg, 3)
	if len(reply) != StatusReplySize {
		t.Fatalf("expected length %d, got %d", StatusReplySize, len(reply))
	}
}

func TestBuildStatusReply_Magic(t *testing.T) {
	cfg := newTestConfig()
	reply := buildStatusReply(cfg, 3)
	if string(reply[0:4]) != magicStatus {
		t.Fatalf("magic: got %q, want %q", reply[0:4], magicStatus)
	}
}

func TestBuildStatusReply_ID(t *testing.T) {
	cfg := newTestConfig()
	reply := buildStatusReply(cfg, 3)
	// bytes 4–9 hold a zero-padded 5-digit decimal ID
	got := string(reply[4:9])
	if got != "12345" {
		t.Fatalf("id: got %q, want %q", got, "12345")
	}
}

func TestBuildStatusReply_Count(t *testing.T) {
	cfg := newTestConfig()
	reply := buildStatusReply(cfg, 7)
	// last 3 bytes hold the zero-padded client count
	got := string(reply[39:42])
	if got != "007" {
		t.Fatalf("count: got %q, want %q", got, "007")
	}
}

func TestBuildStatusReply_NamePadded(t *testing.T) {
	cfg := newTestConfig()
	reply := buildStatusReply(cfg, 0)
	// bytes 9–25 hold the 16-byte name field, space-padded
	name := reply[9:25]
	if len(name) != 16 {
		t.Fatalf("name field length: got %d, want 16", len(name))
	}
	if string(name[:7]) != "TestNet" {
		t.Fatalf("name prefix: got %q", name[:7])
	}
	// rest should be spaces
	for i := 7; i < 16; i++ {
		if name[i] != ' ' {
			t.Fatalf("name[%d] = %q, want space", i, name[i])
		}
	}
}

func TestBuildStatusReply_DescriptionPadded(t *testing.T) {
	cfg := newTestConfig()
	reply := buildStatusReply(cfg, 0)
	// bytes 25–39 hold the 14-byte description field, space-padded
	desc := reply[25:39]
	if len(desc) != 14 {
		t.Fatalf("description field length: got %d, want 14", len(desc))
	}
	if string(desc[:10]) != "A test net" {
		t.Fatalf("description prefix: got %q", desc[:10])
	}
}
