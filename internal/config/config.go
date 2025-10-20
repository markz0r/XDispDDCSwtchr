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
	if err != nil { return nil, fmt.Errorf("read settings: %w", err) }
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("parse settings: %w", err)
	}
	return &s, nil
}
