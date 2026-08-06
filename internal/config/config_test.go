package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
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

func writeConfig(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
