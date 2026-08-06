package profiles

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

type VerificationMode string

const (
	VerifyNone           VerificationMode = "none"
	VerifyImmediate      VerificationMode = "immediate"
	VerifyAfterReconnect VerificationMode = "after-reconnect"
)

type EDIDMatcher struct {
	Manufacturer string
	ProductCode  uint16
	ModelNames   []string
}

type Profile struct {
	Name          string
	Match         EDIDMatcher
	Inputs        map[monitor.Input]uint16
	ReadSupported bool
	RetryCount    int
	ReplyDelay    time.Duration
	WriteDelay    time.Duration
	Verification  VerificationMode
}

var builtins = []Profile{
	{
		Name:          "Dell S3423DWC",
		Match:         EDIDMatcher{Manufacturer: "DEL", ProductCode: 0xd155, ModelNames: []string{"DELL S3423DWC", "S3423DWC"}},
		Inputs:        map[monitor.Input]uint16{},
		ReadSupported: true,
		RetryCount:    3,
		ReplyDelay:    50 * time.Millisecond,
		WriteDelay:    150 * time.Millisecond,
		Verification:  VerifyImmediate,
	},
	{
		Name:          "Dell U4025QW",
		Match:         EDIDMatcher{Manufacturer: "DEL", ProductCode: 0x4308, ModelNames: []string{"DELL U4025QW", "U4025QW"}},
		Inputs:        map[monitor.Input]uint16{},
		ReadSupported: true,
		RetryCount:    3,
		ReplyDelay:    50 * time.Millisecond,
		WriteDelay:    150 * time.Millisecond,
		Verification:  VerifyImmediate,
	},
}

type Registry struct {
	profiles []Profile
}

func NewRegistry(overrides ...Profile) (*Registry, error) {
	source := builtins
	if len(overrides) > 0 {
		source = overrides
	}
	r := &Registry{profiles: make([]Profile, len(source))}
	copy(r.profiles, source)
	if err := r.validate(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Registry) Match(manufacturer string, productCode uint16, model string) (*Profile, bool) {
	manufacturer = normalise(manufacturer)
	model = normalise(model)
	for i := range r.profiles {
		p := &r.profiles[i]
		if normalise(p.Match.Manufacturer) != manufacturer || p.Match.ProductCode != productCode {
			continue
		}
		if len(p.Match.ModelNames) == 0 {
			return p, true
		}
		for _, candidate := range p.Match.ModelNames {
			if normalise(candidate) == model {
				return p, true
			}
		}
	}
	return nil, false
}

func (r *Registry) Inputs(profileName string) []monitor.InputValue {
	for i := range r.profiles {
		if r.profiles[i].Name != profileName {
			continue
		}
		out := make([]monitor.InputValue, 0, len(r.profiles[i].Inputs))
		for logical, raw := range r.profiles[i].Inputs {
			out = append(out, monitor.InputValue{Logical: logical, Raw: raw})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Logical < out[j].Logical })
		return out
	}
	return nil
}

func (r *Registry) ByName(name string) (*Profile, bool) {
	for i := range r.profiles {
		if r.profiles[i].Name == name {
			return &r.profiles[i], true
		}
	}
	return nil, false
}

func (p *Profile) Resolve(input monitor.Input) (monitor.InputValue, error) {
	raw, ok := p.Inputs[input]
	if !ok {
		return monitor.InputValue{}, fmt.Errorf("%w: %s", monitor.ErrUnknownInput, input)
	}
	return monitor.InputValue{Logical: input, Raw: raw}, nil
}

func (p *Profile) Logical(raw uint16) (monitor.InputValue, bool) {
	for logical, candidate := range p.Inputs {
		if candidate == raw {
			return monitor.InputValue{Logical: logical, Raw: raw}, true
		}
	}
	return monitor.InputValue{Raw: raw}, false
}

func (r *Registry) validate() error {
	matchers := map[string]string{}
	for _, p := range r.profiles {
		if p.Name == "" || p.Match.Manufacturer == "" || p.Match.ProductCode == 0 {
			return fmt.Errorf("invalid profile identity: %q", p.Name)
		}
		key := fmt.Sprintf("%s:%04x", normalise(p.Match.Manufacturer), p.Match.ProductCode)
		if prior, ok := matchers[key]; ok {
			return fmt.Errorf("duplicate profile matcher %s: %s and %s", key, prior, p.Name)
		}
		matchers[key] = p.Name
		seenRaw := map[uint16]monitor.Input{}
		for logical, raw := range p.Inputs {
			if logical == "" {
				return fmt.Errorf("profile %s has empty logical input", p.Name)
			}
			if prior, ok := seenRaw[raw]; ok {
				return fmt.Errorf("profile %s maps %s and %s to raw 0x%02x", p.Name, prior, logical, raw)
			}
			seenRaw[raw] = logical
		}
	}
	return nil
}

func normalise(value string) string {
	return strings.Join(strings.Fields(strings.ToUpper(strings.TrimSpace(value))), " ")
}
