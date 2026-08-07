package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
	"github.com/markz0r/XDispDDCSwtchr/internal/qualification"
)

func TestLoadStrictConfiguration(t *testing.T) {
	path := writeConfig(t, `{"schema_version":1,"preferred_monitor_id":"mon-test","diagnostic_redaction":false}`)
	settings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if settings.PreferredMonitorID != "mon-test" || settings.DiagnosticRedaction == nil || *settings.DiagnosticRedaction {
		t.Fatalf("unexpected settings: %+v", settings)
	}
}

func TestLoadRejectsUnknownTrailingAndLegacy(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want error
	}{
		{"unknown", `{"schema_version":1,"surprise":true}`, nil},
		{"trailing", `{"schema_version":1} {}`, nil},
		{"legacy", `{"backend":"cli","hotkeys":[]}`, monitor.ErrUnsupportedConfigSchema},
		{"version", `{"schema_version":2}`, monitor.ErrUnsupportedConfigSchema},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tt.raw))
			if err == nil {
				t.Fatal("expected error")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestDefaultsMissingFilesAndPathPrecedence(t *testing.T) {
	if settings, err := Load(""); err != nil || settings.SchemaVersion != 1 || settings.DiagnosticRedaction == nil || !*settings.DiagnosticRedaction {
		t.Fatalf("defaults=%+v err=%v", settings, err)
	}
	missing := filepath.Join(t.TempDir(), "missing.json")
	if _, err := Load(missing); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("directory configuration path was accepted")
	}
	if got, err := ResolvePath("explicit.json"); err != nil || got != "explicit.json" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	t.Setenv("XDISPDDCSWTCHR_CONFIG", "environment.json")
	if got, err := ResolvePath(""); err != nil || got != "environment.json" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	t.Setenv("XDISPDDCSWTCHR_CONFIG", "")
	if got, err := ResolvePath(""); err != nil || filepath.Base(got) != "config.json" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestLoadRejectsEmptyAliases(t *testing.T) {
	for _, raw := range []string{
		`{"schema_version":1,"input_aliases":{"":"hdmi-1"}}`,
		`{"schema_version":1,"input_aliases":{"work":" "}}`,
	} {
		if _, err := Load(writeConfig(t, raw)); err == nil {
			t.Fatalf("accepted invalid aliases: %s", raw)
		}
	}
}

func TestQualificationStorePersistsAndReplacesExactRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	settings := Defaults()
	settings.PreferredMonitorID = "preferred"
	if err := Save(path, settings); err != nil {
		t.Fatal(err)
	}
	store := NewQualificationStore(path)
	record := qualificationRecord("mon-b", 0x11)
	if err := store.Save(record); err != nil {
		t.Fatal(err)
	}
	replacement := qualificationRecord("mon-b", 0x12)
	replacement.Inputs[0].Logical = monitor.InputHDMI2
	if err := store.Save(replacement); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(qualificationRecord("mon-a", 0x0f)); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PreferredMonitorID != "preferred" || len(loaded.UserQualifications) != 2 ||
		loaded.UserQualifications[0].MonitorID != "mon-a" || loaded.UserQualifications[1].Inputs[0].Raw != 0x12 {
		t.Fatalf("unexpected persisted settings: %+v", loaded)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("configuration permissions are %o", info.Mode().Perm())
	}
	docked := qualificationRecord("mon-b", 0x0f)
	docked.Connector = "dock"
	docked.Inputs[0].Logical = monitor.InputDisplayPort1
	if err := store.Save(docked); err != nil {
		t.Fatal(err)
	}
	loaded, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.UserQualifications) != 3 {
		t.Fatalf("separate endpoint qualification was overwritten: %+v", loaded.UserQualifications)
	}
}

func TestSaveRejectsInvalidQualificationsAndEmptyPath(t *testing.T) {
	settings := Defaults()
	settings.UserQualifications = []qualification.Record{qualificationRecord("mon", 0x100)}
	if err := Save(filepath.Join(t.TempDir(), "config.json"), settings); err == nil {
		t.Fatal("invalid qualification was saved")
	}
	if err := Save("", Defaults()); err == nil {
		t.Fatal("empty path was accepted")
	}
}

func qualificationRecord(id string, raw uint16) qualification.Record {
	return qualification.Record{
		MonitorID: id, EDIDSHA256: strings.Repeat("a", 64), Manufacturer: "DEL", ProductCode: 1,
		Model: "Display", Connector: "direct", BackendName: "fake",
		Inputs:           []monitor.InputValue{{Logical: monitor.InputHDMI1, Raw: raw}},
		CapabilitySHA256: strings.Repeat("b", 64), AcceptedAt: time.Unix(1, 0).UTC(),
	}
}

func writeConfig(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
