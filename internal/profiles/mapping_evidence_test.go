package profiles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

func TestBuiltinMappingsMatchTrackedCapabilityEvidence(t *testing.T) {
	type evidenceProfile struct {
		Profile      string                   `json:"profile"`
		Manufacturer string                   `json:"manufacturer"`
		ProductCode  uint16                   `json:"product_code"`
		Inputs       map[monitor.Input]uint16 `json:"inputs"`
	}
	var evidence struct {
		SchemaVersion int               `json:"schema_version"`
		MappingClaim  bool              `json:"mapping_claim"`
		SupportClaim  bool              `json:"support_claim"`
		Profiles      []evidenceProfile `json:"profiles"`
	}
	path := filepath.Join("..", "..", "qualification", "evidence", "2026-08-07-user-provided-vcp-capabilities.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.SchemaVersion != 1 || !evidence.MappingClaim || evidence.SupportClaim || len(evidence.Profiles) != 3 {
		t.Fatalf("invalid capability-evidence header: %+v", evidence)
	}
	registry, err := NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range evidence.Profiles {
		profile, ok := registry.ByName(entry.Profile)
		if !ok {
			t.Fatalf("evidence refers to unknown profile %q", entry.Profile)
		}
		if normalise(profile.Match.Manufacturer) != normalise(entry.Manufacturer) || !matchesProductCode(profile.Match, entry.ProductCode) {
			t.Fatalf("%s identity does not include %s:%04x", entry.Profile, entry.Manufacturer, entry.ProductCode)
		}
		if len(profile.Inputs) != len(entry.Inputs) {
			t.Fatalf("%s profile inputs=%+v evidence=%+v", entry.Profile, profile.Inputs, entry.Inputs)
		}
		for logical, value := range entry.Inputs {
			if profile.Inputs[logical] != value {
				t.Fatalf("%s %s=0x%02x evidence=0x%02x", entry.Profile, logical, profile.Inputs[logical], value)
			}
		}
	}
}
