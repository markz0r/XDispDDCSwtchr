package edid

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

func StableID(info Info, topology string, numericSerialUnique bool) (string, monitor.IdentityState, error) {
	manufacturer := normalise(info.Manufacturer)
	model := normalise(info.ModelName)
	textSerial := normaliseSerial(info.TextSerial)
	if manufacturer == "" || info.ProductCode == 0 {
		return "", monitor.IdentityInvalidEDID, fmt.Errorf("insufficient EDID identity")
	}

	var canonical string
	state := monitor.IdentityHardwareStable
	switch {
	case info.NumericSerial != 0 && numericSerialUnique:
		canonical = fmt.Sprintf("numeric|%s|%04x|%08x", manufacturer, info.ProductCode, info.NumericSerial)
	case textSerial != "":
		canonical = fmt.Sprintf("text|%s|%04x|%s", manufacturer, info.ProductCode, textSerial)
	case strings.TrimSpace(topology) != "":
		canonical = fmt.Sprintf("topology|%s|%04x|%s|%s", manufacturer, info.ProductCode, model, normalise(topology))
		state = monitor.IdentityTopologyScoped
	default:
		return "", monitor.IdentityAmbiguous, monitor.ErrAmbiguousMonitor
	}

	sum := sha256.Sum256([]byte(canonical))
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:16])
	return "mon-" + strings.ToLower(encoded), state, nil
}

func SHA256(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func normalise(value string) string {
	return strings.Join(strings.Fields(strings.ToUpper(strings.TrimSpace(value))), " ")
}

func normaliseSerial(value string) string {
	s := normalise(value)
	switch s {
	case "", "0", "00000000", "UNKNOWN", "N/A", "NONE":
		return ""
	default:
		return s
	}
}
