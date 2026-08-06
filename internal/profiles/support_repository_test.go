package profiles

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

func TestTrackedSupportRecordsHaveMatchingEvidenceAndProfileMappings(t *testing.T) {
	matrix, err := LoadSupportMatrix()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	for _, record := range matrix.Records {
		record := record
		t.Run(record.ID, func(t *testing.T) {
			profile, ok := registry.ByName(record.Profile)
			if !ok {
				t.Fatalf("support record refers to unknown profile %q", record.Profile)
			}
			if profile.Verification != record.Verification {
				t.Fatalf("record verification %q differs from profile %q", record.Verification, profile.Verification)
			}
			for _, value := range record.Inputs {
				if _, err := profile.Resolve(monitor.Input(value)); err != nil {
					t.Fatalf("support record exposes an absent profile mapping %q: %v", value, err)
				}
			}
			evidence, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(record.QualificationRecord)))
			if err != nil {
				t.Fatalf("read qualification evidence: %v", err)
			}
			digest := sha256.Sum256(evidence)
			if got := hex.EncodeToString(digest[:]); got != record.EvidenceSHA256 {
				t.Fatalf("evidence digest %s, support matrix has %s", got, record.EvidenceSHA256)
			}
		})
	}
}
