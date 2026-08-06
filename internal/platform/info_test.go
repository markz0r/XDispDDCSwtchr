package platform

import (
	"strings"
	"testing"
)

func TestParsePlistDictionary(t *testing.T) {
	values, err := parsePlistDictionary(strings.NewReader(`<?xml version="1.0"?><plist><dict><key>ProductVersion</key><string>26.6</string><key>ProductBuildVersion</key><string>25G72</string></dict></plist>`))
	if err != nil {
		t.Fatal(err)
	}
	if values["ProductVersion"] != "26.6" || values["ProductBuildVersion"] != "25G72" {
		t.Fatalf("unexpected values: %#v", values)
	}
}

func TestCurrent(t *testing.T) {
	info, err := Current()
	if err != nil {
		t.Fatal(err)
	}
	if info.OS == "" || info.Architecture == "" {
		t.Fatalf("incomplete platform info: %+v", info)
	}
}
