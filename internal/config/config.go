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
	"sort"
	"strings"
	"sync"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
	"github.com/markz0r/XDispDDCSwtchr/internal/qualification"
)

const SchemaVersion = 1

type Settings struct {
	SchemaVersion       int                    `json:"schema_version"`
	PreferredMonitorID  string                 `json:"preferred_monitor_id,omitempty"`
	InputAliases        map[string]string      `json:"input_aliases,omitempty"`
	DebugLogPath        string                 `json:"debug_log_path,omitempty"`
	DiagnosticRedaction *bool                  `json:"diagnostic_redaction,omitempty"`
	UserQualifications  []qualification.Record `json:"user_qualifications,omitempty"`
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
	if err := validate(settings); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func Save(path string, settings Settings) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("configuration path is empty")
	}
	if err := validate(settings); err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}
	encoded = append(encoded, '\n')
	temporary, err := os.CreateTemp(directory, ".xdispddcswtchr-config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure temporary configuration: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary configuration: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary configuration: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary configuration: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace configuration: %w", err)
	}
	return nil
}

func validate(settings Settings) error {
	if settings.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: got schema_version %d, require %d", monitor.ErrUnsupportedConfigSchema, settings.SchemaVersion, SchemaVersion)
	}
	for alias, input := range settings.InputAliases {
		if strings.TrimSpace(alias) == "" || strings.TrimSpace(input) == "" {
			return fmt.Errorf("input_aliases contains an empty alias or value")
		}
	}
	seen := make(map[string]bool, len(settings.UserQualifications))
	for _, record := range settings.UserQualifications {
		if err := qualification.Validate(record); err != nil {
			return err
		}
		key := qualification.Key(record)
		if seen[key] {
			return fmt.Errorf("duplicate user qualification for monitor %q endpoint %q", record.MonitorID, record.Connector)
		}
		seen[key] = true
	}
	return nil
}

type QualificationStore struct {
	mu   sync.Mutex
	path string
}

func NewQualificationStore(path string) *QualificationStore {
	return &QualificationStore{path: path}
}

func (s *QualificationStore) Save(record qualification.Record) error {
	if err := qualification.Validate(record); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	settings, err := Load(s.path)
	if err != nil {
		return err
	}
	replaced := false
	for index := range settings.UserQualifications {
		if qualification.Key(settings.UserQualifications[index]) == qualification.Key(record) {
			settings.UserQualifications[index] = record
			replaced = true
			break
		}
	}
	if !replaced {
		settings.UserQualifications = append(settings.UserQualifications, record)
	}
	sort.Slice(settings.UserQualifications, func(i, j int) bool {
		return qualification.Key(settings.UserQualifications[i]) < qualification.Key(settings.UserQualifications[j])
	})
	return Save(s.path, settings)
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
