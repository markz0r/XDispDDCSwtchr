package capabilities

import (
	"errors"
	"strings"
	"testing"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

func TestParseInputSourceValues(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		advertised bool
		values     []uint16
	}{
		{
			name:       "spaced",
			raw:        "(prot(monitor)type(LCD)vcp(10 60(0F 10 11 12) DF))",
			advertised: true, values: []uint16{0x0f, 0x10, 0x11, 0x12},
		},
		{
			name:       "unspaced-and-unrelated-nesting",
			raw:        "(VCP(0203(10 00)5260(191B1112)86(01 02)))",
			advertised: true, values: []uint16{0x19, 0x1b, 0x11, 0x12},
		},
		{
			name:       "duplicates",
			raw:        "(vcp(60(11 12 11) 60(12 0F)))",
			advertised: true, values: []uint16{0x11, 0x12, 0x0f},
		},
		{name: "feature-without-values", raw: "(vcp(10 60 DF))", advertised: true, values: []uint16{}},
		{name: "no-input-feature", raw: "(vcp(10 12 DF))", values: []uint16{}},
		{name: "no-vcp-segment", raw: "(prot(monitor)cmds(01 02 03))", values: []uint16{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			if got.InputSourceAdvertised != tt.advertised || !equalValues(got.InputValues, tt.values) {
				t.Fatalf("got=%+v want advertised=%t values=%x", got, tt.advertised, tt.values)
			}
		})
	}
}

func TestParseRejectsMalformedAndOversizedData(t *testing.T) {
	for _, raw := range []string{"(vcp(60(11 1)))", "(vcp(60(11)", "(vcp(60[11]))"} {
		if _, err := Parse(raw); !errors.Is(err, monitor.ErrMalformedReply) {
			t.Fatalf("Parse(%q) error=%v", raw, err)
		}
	}
	if _, err := Parse(strings.Repeat("x", MaxStringLength+1)); !errors.Is(err, monitor.ErrMalformedReply) {
		t.Fatalf("oversize error=%v", err)
	}
}

func TestStandardInputOnlyMapsMCCSDefinedApplicationInputs(t *testing.T) {
	for raw, want := range map[uint16]monitor.Input{
		0x0f: monitor.InputDisplayPort1,
		0x10: monitor.InputDisplayPort2,
		0x11: monitor.InputHDMI1,
		0x12: monitor.InputHDMI2,
	} {
		got, ok := StandardInput(raw)
		if !ok || got != want {
			t.Fatalf("StandardInput(0x%02x)=(%q,%t), want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []uint16{0x01, 0x19, 0x1b, 0xff} {
		if got, ok := StandardInput(raw); ok || got != "" {
			t.Fatalf("vendor/non-product value 0x%02x mapped as %q", raw, got)
		}
	}
}

func FuzzParse(f *testing.F) {
	f.Add("(prot(monitor)vcp(60(0F 11 12)))")
	f.Add("(vcp(0203(10 00)5260(191B1112)))")
	f.Fuzz(func(t *testing.T, raw string) {
		_, _ = Parse(raw)
	})
}

func equalValues(left, right []uint16) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
