package reflector

import (
	"fmt"

	"github.com/siteworxpro/ysf-reflector-go/internal/config"
)

// YSF packet magic byte prefixes (4 bytes each).
const (
	magicPoll   = "YSFP" // keepalive poll — node ↔ reflector
	magicData   = "YSFD" // voice/data frame — node → reflector → all others
	magicUnlink = "YSFU" // unlink request — node → reflector
	magicStatus = "YSFS" // status query — node → reflector; reflector responds
	magicOption = "YSFO" // option/DG-ID message — silently ignored
	magicInfo   = "YSFI" // info message    — silently ignored
)

// Packet size constants defined by the YSF protocol.
const (
	// PollSize 4 magic + 10 callsign = 14 bytes.
	PollSize = 14
	// DataSize 4 magic + 10 src + 10 dst + 10 downstream + 1 path + 1 serial + 120 payload = 155 bytes.
	DataSize = 155
	// UnlinkSize 4 magic + 10 callsign = 14 bytes.
	UnlinkSize = 14
	// StatusReplySize "YSFS" + 5-digit ID + 16-char name + 14-char description + 3-digit count = 42 bytes.
	StatusReplySize = 42
)

// parsePollCallsign extracts and trims the client callsign from a YSFP packet.
func parsePollCallsign(pkt []byte) string {
	return trimCallsign(pkt[4:14])
}

// parseDataSrcCallsign extracts the source callsign from a YSFD packet.
func parseDataSrcCallsign(pkt []byte) string {
	return trimCallsign(pkt[4:14])
}

// parseUnlinkCallsign extracts the callsign from a YSFU packet.
func parseUnlinkCallsign(pkt []byte) string {
	return trimCallsign(pkt[4:14])
}

// trimCallsign converts a space- or NUL-padded 10-byte callsign field to a plain string.
func trimCallsign(b []byte) string {
	s := string(b)
	for i, c := range s {
		if c == ' ' || c == 0 {
			return s[:i]
		}
	}
	return s
}

// buildPollReply constructs the 14-byte YSFP reply using the reflector's own callsign.
func buildPollReply(callsign []byte) []byte {
	pkt := make([]byte, PollSize)
	copy(pkt[0:4], magicPoll)
	copy(pkt[4:14], callsign)
	return pkt
}

// buildStatusReply constructs the 42-byte YSFS status response.
// Format: "YSFS" + %05u(id) + %-16s(name) + %-14s(description) + %03u(count)
func buildStatusReply(cfg *config.Config, count int) []byte {
	pkt := make([]byte, StatusReplySize)
	copy(pkt[0:4], magicStatus)
	copy(pkt[4:9], fmt.Sprintf("%05d", cfg.ID))
	copy(pkt[9:25], config.PaddedField(cfg.Name, 16))
	copy(pkt[25:39], config.PaddedField(cfg.Description, 14))
	copy(pkt[39:42], fmt.Sprintf("%03d", count))
	return pkt
}
