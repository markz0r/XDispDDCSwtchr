package ddc

import (
	"errors"
	"reflect"
	"testing"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

func TestEncodeGetVCPGolden(t *testing.T) {
	want := []byte{0x51, 0x82, 0x01, 0x60, 0xdc}
	if got := EncodeGetVCP(VCPInputSource); !reflect.DeepEqual(got, want) {
		t.Fatalf("got % x want % x", got, want)
	}
}

func TestEncodeSetVCPGolden(t *testing.T) {
	want := []byte{0x51, 0x84, 0x03, 0x60, 0x00, 0x11, 0xc9}
	if got := EncodeSetVCP(VCPInputSource, 0x11); !reflect.DeepEqual(got, want) {
		t.Fatalf("got % x want % x", got, want)
	}
}

func TestParseGetVCPReply(t *testing.T) {
	reply := validReply()
	got, err := ParseGetVCPReply(reply, VCPInputSource)
	if err != nil {
		t.Fatal(err)
	}
	if got.Current != 0x11 || got.Maximum != 0x1b || got.Continuous {
		t.Fatalf("unexpected reply: %+v", got)
	}
}

func TestParseGetVCPReplyCoversProtocolMetadataAndErrors(t *testing.T) {
	continuous := validReply()
	continuous[5] = 0
	setReplyChecksum(continuous)
	parsed, err := ParseGetVCPReply(continuous, VCPInputSource)
	if err != nil || !parsed.Continuous {
		t.Fatalf("continuous=%v err=%v", parsed.Continuous, err)
	}

	tests := []struct {
		name string
		edit func([]byte)
		want error
	}{
		{"payload-length", func(reply []byte) { reply[1] = 0x87 }, monitor.ErrMalformedReply},
		{"command", func(reply []byte) { reply[2] = 0x03 }, monitor.ErrMalformedReply},
		{"unsupported-result", func(reply []byte) { reply[3] = 0x01 }, monitor.ErrReadUnsupported},
		{"wrong-vcp", func(reply []byte) { reply[4] = 0x62 }, monitor.ErrMalformedReply},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reply := validReply()
			tt.edit(reply)
			setReplyChecksum(reply)
			if _, err := ParseGetVCPReply(reply, VCPInputSource); !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestNormalizeInputSourceValue(t *testing.T) {
	for input, want := range map[uint16]uint16{0x1111: 0x11, 0x1919: 0x19, 0x001b: 0x1b, 0x1234: 0x1234} {
		if got := NormalizeInputSourceValue(input); got != want {
			t.Fatalf("NormalizeInputSourceValue(0x%04x) = 0x%04x, want 0x%04x", input, got, want)
		}
	}
}

func TestParseGetVCPReplyRejectsCorruption(t *testing.T) {
	reply := []byte{0x6e, 0x88, 0x02, 0x00, 0x60, 0x01, 0x00, 0x1b, 0x00, 0x11, 0xff}
	if _, err := ParseGetVCPReply(reply, VCPInputSource); !errors.Is(err, monitor.ErrChecksum) {
		t.Fatalf("got %v", err)
	}
	if _, err := ParseGetVCPReply(reply[:4], VCPInputSource); !errors.Is(err, monitor.ErrMalformedReply) {
		t.Fatalf("got %v", err)
	}
}

func FuzzParseGetVCPReply(f *testing.F) {
	f.Add(validReply())
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _ = ParseGetVCPReply(raw, VCPInputSource)
	})
}

func validReply() []byte {
	reply := []byte{0x6e, 0x88, 0x02, 0x00, 0x60, 0x01, 0x00, 0x1b, 0x00, 0x11, 0x00}
	setReplyChecksum(reply)
	return reply
}

func setReplyChecksum(reply []byte) {
	length := int(reply[1] & 0x7f)
	if 2+length < len(reply) {
		reply[2+length] = checksum(DisplayReadByte, append([]byte{HostAddress}, reply[1:2+length]...))
	}
}
