package monitor

import (
	"errors"
	"testing"
)

func TestStableErrorsSupportErrorsIs(t *testing.T) {
	err := errors.Join(ErrDisconnected, errors.New("native detail"))
	if !errors.Is(err, ErrDisconnected) {
		t.Fatal("wrapped error did not preserve category")
	}
}

func TestInputNamesAreStable(t *testing.T) {
	want := []Input{InputDisplayPort1, InputDisplayPort2, InputHDMI1, InputHDMI2, InputUSBC1, InputUSBC2, InputThunderbolt1}
	seen := map[Input]bool{}
	for _, input := range want {
		if input == "" || seen[input] {
			t.Fatalf("invalid or duplicate input %q", input)
		}
		seen[input] = true
	}
}
