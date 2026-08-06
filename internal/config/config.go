package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

const SchemaVersion = 1

type Settings struct {
	SchemaVersion       int               `json:"schema_version"`
	PreferredMonitorID  string            `json:"preferred_monitor_id,omitempty"`
	InputAliases        map[string]string `json:"input_aliases,omitempty"`
	DebugLogPath        string            `json:"debug_log_path,omitempty"`
	DiagnosticRedaction *bool             `json:"diagnostic_redaction,omitempty"`
}

func Defaults() Settings {
	redact := true
	return Settings{SchemaVersion: SchemaVersion, DiagnosticRedaction: &redact}
}

func Load(path string) (Settings, error) {
	settings := Defaults()
	if strings.TrimSpace(path) == "" {
		return settings, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("read configuration: %w", err)
	}
	if legacySchema(raw) {
		return Settings{}, fmt.Errorf("%w: hotkey/PIP/external-tool settings must be removed", monitor.ErrUnsupportedConfigSchema)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		return Settings{}, fmt.Errorf("parse configuration: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Settings{}, fmt.Errorf("parse configuration: trailing JSON value")
		}
		return Settings{}, fmt.Errorf("parse configuration: %w", err)
	}
	if settings.SchemaVersion != SchemaVersion {
		return Settings{}, fmt.Errorf("%w: got schema_version %d, require %d", monitor.ErrUnsupportedConfigSchema, settings.SchemaVersion, SchemaVersion)
	}
	for alias, input := range settings.InputAliases {
		if strings.TrimSpace(alias) == "" || strings.TrimSpace(input) == "" {
			return Settings{}, fmt.Errorf("input_aliases contains an empty alias or value")
		}
	}
	return settings, nil
}

func ResolvePath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if env := os.Getenv("XDISPDDCSWTCHR_CONFIG"); env != "" {
		return env, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	name := "config.json"
	if runtime.GOOS == "windows" {
		return filepath.Join(dir, "XDispDDCSwtchr", name), nil
	}
	return filepath.Join(dir, "xdispddcswtchr", name), nil
}

func legacySchema(raw []byte) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	for _, key := range []string{"hotkeys", "models", "monitors", "backend", "cli", "pip"} {
		if _, ok := fields[key]; ok {
			return true
		}
	}
	return false
}
