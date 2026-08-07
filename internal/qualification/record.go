package qualification

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
	"github.com/markz0r/XDispDDCSwtchr/internal/profiles"
)

const profilePrefix = "user-qualified/"

// Record is a local user's explicit acceptance of input mappings for one exact
// monitor identity and connection endpoint. It is not release qualification
// evidence and must never be added to the embedded support matrix implicitly.
type Record struct {
	MonitorID        string               `json:"monitor_id"`
	EDIDSHA256       string               `json:"edid_sha256"`
	Manufacturer     string               `json:"manufacturer"`
	ProductCode      uint16               `json:"product_code"`
	Model            string               `json:"model"`
	Connector        string               `json:"connector"`
	BackendName      string               `json:"backend_name"`
	Inputs           []monitor.InputValue `json:"inputs"`
	CapabilitySHA256 string               `json:"capability_sha256"`
	AcceptedAt       time.Time            `json:"accepted_at"`
}

func New(descriptor monitor.Descriptor, inputs []monitor.InputValue, capabilityRaw string, acceptedAt time.Time) (Record, error) {
	if descriptor.IdentityState == monitor.IdentityAmbiguous {
		return Record{}, monitor.ErrAmbiguousMonitor
	}
	if descriptor.IdentityState == monitor.IdentityInvalidEDID {
		return Record{}, monitor.ErrUnsupportedMonitor
	}
	digest := sha256.Sum256([]byte(capabilityRaw))
	record := Record{
		MonitorID: descriptor.ID, EDIDSHA256: descriptor.EDIDSHA256,
		Manufacturer: descriptor.Manufacturer, ProductCode: descriptor.ProductCode, Model: descriptor.Model,
		Connector: descriptor.Connector, BackendName: descriptor.BackendName,
		Inputs: append([]monitor.InputValue(nil), inputs...), CapabilitySHA256: hex.EncodeToString(digest[:]), AcceptedAt: acceptedAt.UTC(),
	}
	sort.Slice(record.Inputs, func(i, j int) bool { return record.Inputs[i].Logical < record.Inputs[j].Logical })
	if err := Validate(record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func Validate(record Record) error {
	if strings.TrimSpace(record.MonitorID) == "" || strings.TrimSpace(record.EDIDSHA256) == "" ||
		strings.TrimSpace(record.Manufacturer) == "" || record.ProductCode == 0 ||
		strings.TrimSpace(record.Connector) == "" || strings.TrimSpace(record.BackendName) == "" ||
		len(record.Inputs) == 0 || record.AcceptedAt.IsZero() {
		return fmt.Errorf("invalid user qualification: exact identity, endpoint, mappings, and acceptance time are required")
	}
	if !validDigest(record.EDIDSHA256) || !validDigest(record.CapabilitySHA256) {
		return fmt.Errorf("invalid user qualification: EDID and capability digests must be SHA-256")
	}
	seenLogical := make(map[monitor.Input]bool, len(record.Inputs))
	seenRaw := make(map[uint16]bool, len(record.Inputs))
	for _, value := range record.Inputs {
		if !KnownInput(value.Logical) || value.Raw > 0xff || seenLogical[value.Logical] || seenRaw[value.Raw] {
			return fmt.Errorf("invalid user qualification input mapping %q=0x%04x", value.Logical, value.Raw)
		}
		seenLogical[value.Logical] = true
		seenRaw[value.Raw] = true
	}
	return nil
}

func Matches(record Record, descriptor monitor.Descriptor) bool {
	return record.MonitorID == descriptor.ID && record.EDIDSHA256 == descriptor.EDIDSHA256 &&
		record.Manufacturer == descriptor.Manufacturer && record.ProductCode == descriptor.ProductCode &&
		record.Model == descriptor.Model && record.Connector == descriptor.Connector && record.BackendName == descriptor.BackendName
}

// Key keeps separate decisions for the same physical monitor when it is used
// through different connectors or backends.
func Key(record Record) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%04x\x00%s\x00%s\x00%s", record.MonitorID, record.EDIDSHA256,
		record.Manufacturer, record.ProductCode, record.Model, record.Connector, record.BackendName)
}

func DescriptorKey(descriptor monitor.Descriptor) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%04x\x00%s\x00%s\x00%s", descriptor.ID, descriptor.EDIDSHA256,
		descriptor.Manufacturer, descriptor.ProductCode, descriptor.Model, descriptor.Connector, descriptor.BackendName)
}

func MatchesCapabilities(record Record, raw string) bool {
	digest := sha256.Sum256([]byte(raw))
	return record.CapabilitySHA256 == hex.EncodeToString(digest[:])
}

func Profile(record Record, descriptor monitor.Descriptor, base *profiles.Profile) *profiles.Profile {
	profile := profiles.Profile{
		Name:          ProfileName(descriptor),
		Inputs:        make(map[monitor.Input]uint16, len(record.Inputs)),
		ReadSupported: true,
		RetryCount:    3,
		ReplyDelay:    50 * time.Millisecond,
		WriteDelay:    150 * time.Millisecond,
		Verification:  profiles.VerifyImmediate,
	}
	if base != nil {
		profile.ReadSupported = base.ReadSupported
		profile.RetryCount = base.RetryCount
		profile.ReplyDelay = base.ReplyDelay
		profile.WriteDelay = base.WriteDelay
		profile.Verification = base.Verification
	}
	for _, value := range record.Inputs {
		profile.Inputs[value.Logical] = value.Raw
	}
	return &profile
}

func ProfileName(descriptor monitor.Descriptor) string {
	name := strings.TrimSpace(descriptor.Model)
	if name == "" {
		name = descriptor.ID
	}
	return profilePrefix + name
}

func KnownInput(input monitor.Input) bool {
	switch input {
	case monitor.InputDisplayPort1, monitor.InputDisplayPort2, monitor.InputHDMI1, monitor.InputHDMI2,
		monitor.InputUSBC1, monitor.InputUSBC2, monitor.InputThunderbolt1:
		return true
	default:
		return false
	}
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
