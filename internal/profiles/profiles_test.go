package profiles

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

func TestDellProfilesMatchExactEDIDIdentity(t *testing.T) {
	r, err := NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		product uint16
		model   string
		name    string
	}{
		{0xd155, "DELL S3423DWC", "Dell S3423DWC"},
		{0x4308, "DELL U4025QW", "Dell U4025QW"},
	}
	for _, tt := range tests {
		p, ok := r.Match("DEL", tt.product, tt.model)
		if !ok || p.Name != tt.name {
			t.Fatalf("product=%04x model=%q got=%+v ok=%v", tt.product, tt.model, p, ok)
		}
	}
	if _, ok := r.Match("DEL", 0x4308, "some other display"); ok {
		t.Fatal("model-name secondary check was bypassed")
	}
	for _, name := range []string{"Dell S3423DWC", "Dell U4025QW"} {
		profile, _ := r.ByName(name)
		if len(profile.Inputs) != 0 {
			t.Fatalf("%s contains a mapping without tracked hardware evidence: %+v", name, profile.Inputs)
		}
	}
}

func TestSupportMatrixIsStrictAndCommitScoped(t *testing.T) {
	commit := strings.Repeat("a", 40)
	edidHash := strings.Repeat("b", 64)
	evidenceHash := strings.Repeat("c", 64)
	valid := `{
  "schema_version": 1,
  "records": [{
    "id":"record-1", "os":"darwin", "architecture":"arm64", "os_version":"26.6", "os_build":"25G72",
		"backend":"macos-native", "application_commit":"` + commit + `", "profile":"Dell U4025QW",
		"edid_sha256":"` + edidHash + `", "connector":"external-dcpext0", "inputs":["hdmi-1"], "verification":"immediate",
		"qualification_record":"qualification/evidence/record.json", "evidence_sha256":"` + evidenceHash + `"
  }]
}`
	matrix, err := ParseSupportMatrix([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	platform := Platform{OS: "darwin", Architecture: "arm64", Version: "26.6", Build: "25G72", ApplicationCommit: commit}
	if _, ok := matrix.Match(platform, "macos-native", "Dell U4025QW", edidHash, "external-dcpext0"); !ok {
		t.Fatal("exact support record did not match")
	}
	platform.ApplicationCommit = "different"
	if _, ok := matrix.Match(platform, "macos-native", "Dell U4025QW", edidHash, "external-dcpext0"); ok {
		t.Fatal("support record authorized a different application commit")
	}

	for _, invalid := range []string{
		strings.Replace(valid, `"application_commit":"`+commit+`", `, "", 1),
		strings.Replace(valid, `"schema_version": 1`, `"schema_version": 1, "unknown": true`, 1),
		valid + `{}`,
	} {
		if _, err := ParseSupportMatrix([]byte(invalid)); err == nil {
			t.Fatalf("accepted invalid support matrix: %s", invalid)
		}
	}
}

func TestProfileResolution(t *testing.T) {
	p := Profile{
		Name: "test", Match: EDIDMatcher{Manufacturer: "TST", ProductCode: 1},
		Inputs: map[monitor.Input]uint16{monitor.InputUSBC1: 0x1b, monitor.InputHDMI1: 0x11},
	}
	value, err := p.Resolve(monitor.InputUSBC1)
	if err != nil || value.Raw != 0x1b {
		t.Fatalf("value=%+v err=%v", value, err)
	}
	logical, ok := p.Logical(0x11)
	if !ok || logical.Logical != monitor.InputHDMI1 {
		t.Fatalf("value=%+v ok=%v", logical, ok)
	}
	r, err := NewRegistry(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Inputs("test"); len(got) != 2 || got[0].Logical != monitor.InputHDMI1 {
		t.Fatalf("unexpected sorted inputs: %+v", got)
	}
	if got := r.Inputs("missing"); got != nil {
		t.Fatalf("missing profile returned inputs: %+v", got)
	}
	if _, ok := r.ByName("missing"); ok {
		t.Fatal("missing profile was found")
	}
	if _, err := p.Resolve(monitor.InputDisplayPort1); err == nil {
		t.Fatal("unknown logical input resolved")
	}
	if value, ok := p.Logical(0xff); ok || value.Raw != 0xff {
		t.Fatalf("unknown raw input resolved: %+v", value)
	}
}

func TestRegistryRejectsAmbiguousOrInvalidProfiles(t *testing.T) {
	valid := func(name string, product uint16) Profile {
		return Profile{Name: name, Match: EDIDMatcher{Manufacturer: "TST", ProductCode: product}, Inputs: map[monitor.Input]uint16{monitor.InputHDMI1: 0x11}}
	}
	tests := []struct {
		name     string
		profiles []Profile
	}{
		{"missing-identity", []Profile{{Name: "bad"}}},
		{"duplicate-matcher", []Profile{valid("one", 1), valid("two", 1)}},
		{"empty-logical", []Profile{{Name: "bad", Match: EDIDMatcher{Manufacturer: "TST", ProductCode: 1}, Inputs: map[monitor.Input]uint16{"": 1}}}},
		{"duplicate-raw", []Profile{{Name: "bad", Match: EDIDMatcher{Manufacturer: "TST", ProductCode: 1}, Inputs: map[monitor.Input]uint16{monitor.InputHDMI1: 1, monitor.InputHDMI2: 1}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewRegistry(tt.profiles...); err == nil {
				t.Fatal("invalid registry was accepted")
			}
		})
	}
}

func TestSupportMatrixRejectsInvalidRecords(t *testing.T) {
	encode := func(schema int, records []map[string]any, extra bool) []byte {
		value := map[string]any{"schema_version": schema, "records": records}
		if extra {
			value["unknown"] = true
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	mutate := func(change func(map[string]any)) map[string]any {
		record := validSupportRecordForTest()
		change(record)
		return record
	}
	tests := []struct {
		name string
		raw  []byte
	}{
		{"schema", encode(2, nil, false)},
		{"unknown-top-level", encode(1, nil, true)},
		{"duplicate-id", encode(1, []map[string]any{validSupportRecordForTest(), validSupportRecordForTest()}, false)},
		{"incomplete", encode(1, []map[string]any{mutate(func(r map[string]any) { delete(r, "os") })}, false)},
		{"invalid-digest", encode(1, []map[string]any{mutate(func(r map[string]any) { r["edid_sha256"] = "short" })}, false)},
		{"unsafe-evidence-path", encode(1, []map[string]any{mutate(func(r map[string]any) { r["qualification_record"] = "../record.json" })}, false)},
		{"invalid-verification", encode(1, []map[string]any{mutate(func(r map[string]any) { r["verification"] = "eventually" })}, false)},
		{"unknown-input", encode(1, []map[string]any{mutate(func(r map[string]any) { r["inputs"] = []string{"vga-9"} })}, false)},
		{"duplicate-input", encode(1, []map[string]any{mutate(func(r map[string]any) { r["inputs"] = []string{"hdmi-1", "hdmi-1"} })}, false)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseSupportMatrix(tt.raw); err == nil {
				t.Fatal("invalid support matrix was accepted")
			}
		})
	}
}

func validSupportRecordForTest() map[string]any {
	return map[string]any{
		"id": "record-1", "os": "darwin", "architecture": "arm64", "os_version": "26.6", "os_build": "25G72",
		"backend": "macos-native", "application_commit": strings.Repeat("a", 40), "profile": "Dell U4025QW",
		"edid_sha256": strings.Repeat("b", 64), "connector": "external", "inputs": []string{"hdmi-1"}, "verification": "immediate",
		"qualification_record": "qualification/evidence/record.json", "evidence_sha256": strings.Repeat("c", 64),
	}
}

func TestEmptySupportMatrixMakesNoClaims(t *testing.T) {
	m, err := LoadSupportMatrix()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Match(Platform{OS: "darwin", Architecture: "arm64", Version: "26.6", Build: "25G72", ApplicationCommit: "commit"}, "macos-native", "Dell U4025QW", "hash", "connector"); ok {
		t.Fatal("empty matrix unexpectedly matched")
	}
}
