package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type BackendKind string

const (
	BackendCLI    BackendKind = "cli"
	BackendNative BackendKind = "native"
)

type VCPCommand struct {
	Code   string            `json:"code"`
	Values map[string]uint16 `json:"values,omitempty"`
}

type PipMode struct {
	ToggleCode   string   `json:"toggle_code"`
	OnValue      uint16   `json:"on_value"`
	OffValue     uint16   `json:"off_value"`
	Sources      []string `json:"sources"`
	Positions    []string `json:"positions,omitempty"`
	Sizes        []string `json:"sizes,omitempty"`
	SwapCode     string   `json:"swap_code,omitempty"`
	SwapValue    uint16   `json:"swap_value,omitempty"`
	SourceCode   string   `json:"source_code,omitempty"`
	PositionCode string   `json:"position_code,omitempty"`
	SizeCode     string   `json:"size_code,omitempty"`
}

type Model struct {
	Name           string            `json:"name"`
	InputSelect    VCPCommand        `json:"input_select"`
	VCPNamedValues map[string]uint16 `json:"vcp_named_values,omitempty"`
	PIP            *PipMode          `json:"pip,omitempty"`
}

type Monitor struct {
	Identifier string `json:"identifier"`
	Model      string `json:"model"`
}

type HotkeyAction struct {
	Keys   string            `json:"keys"`
	Action string            `json:"action"`
	Args   map[string]string `json:"args,omitempty"`
	Target string            `json:"target,omitempty"`
}

type Settings struct {
	Backend  BackendKind    `json:"backend"`
	Models   []Model        `json:"models"`
	Monitors []Monitor      `json:"monitors"`
	Hotkeys  []HotkeyAction `json:"hotkeys"`

	CLI struct {
		LinuxDdcutilPath    string `json:"linux_ddcutil_path,omitempty"`
		MacDdcctlPath       string `json:"mac_ddcctl_path,omitempty"`
		WinControlMyMonPath string `json:"win_controlmymonitor_path,omitempty"`
	} `json:"cli"`
}

func Load(path string) (*Settings, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read settings: %w", err)
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("parse settings: %w", err)
	}

	// Validate configuration
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return &s, nil
}

// Validate checks the configuration for common errors
func (s *Settings) Validate() error {
	// Validate models
	for i, model := range s.Models {
		if model.Name == "" {
			return fmt.Errorf("model[%d]: name is required", i)
		}

		if model.InputSelect.Code == "" {
			return fmt.Errorf("model[%d] (%s): input_select.code is required", i, model.Name)
		}

		// Validate PIP configuration if present
		if model.PIP != nil {
			if err := validatePIPConfig(model.Name, model.PIP, model.VCPNamedValues); err != nil {
				return fmt.Errorf("model[%d] (%s): %w", i, model.Name, err)
			}
		}
	}

	// Validate monitors reference valid models
	modelNames := make(map[string]bool)
	for _, m := range s.Models {
		modelNames[m.Name] = true
	}

	for i, mon := range s.Monitors {
		if mon.Identifier == "" {
			return fmt.Errorf("monitor[%d]: identifier is required", i)
		}
		if mon.Model == "" {
			return fmt.Errorf("monitor[%d] (%s): model is required", i, mon.Identifier)
		}
		if !modelNames[mon.Model] {
			return fmt.Errorf("monitor[%d] (%s): references unknown model '%s'", i, mon.Identifier, mon.Model)
		}
	}

	// Validate hotkeys reference valid actions and targets
	for i, hk := range s.Hotkeys {
		if hk.Keys == "" {
			return fmt.Errorf("hotkey[%d]: keys is required", i)
		}
		if hk.Action == "" {
			return fmt.Errorf("hotkey[%d]: action is required", i)
		}
	}

	return nil
}

func validatePIPConfig(modelName string, pip *PipMode, namedValues map[string]uint16) error {
	if pip.ToggleCode == "" {
		return fmt.Errorf("PIP: toggle_code is required")
	}

	// Validate sources if configured
	if pip.SourceCode != "" && len(pip.Sources) > 0 {
		for _, src := range pip.Sources {
			key := "PIP_Source_" + src
			if _, ok := namedValues[key]; !ok {
				return fmt.Errorf("PIP: source '%s' requires vcp_named_values['%s']", src, key)
			}
		}
	}

	// Validate positions if configured
	if pip.PositionCode != "" && len(pip.Positions) > 0 {
		for _, pos := range pip.Positions {
			key := "PIP_Pos_" + pos
			if _, ok := namedValues[key]; !ok {
				return fmt.Errorf("PIP: position '%s' requires vcp_named_values['%s']", pos, key)
			}
		}
	}

	// Validate sizes if configured
	if pip.SizeCode != "" && len(pip.Sizes) > 0 {
		for _, size := range pip.Sizes {
			key := "PIP_Size_" + size
			if _, ok := namedValues[key]; !ok {
				return fmt.Errorf("PIP: size '%s' requires vcp_named_values['%s']", size, key)
			}
		}
	}

	return nil
}
