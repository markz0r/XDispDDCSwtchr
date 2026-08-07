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

func TestEncodeCapabilitiesRequestGolden(t *testing.T) {
	tests := []struct {
		offset uint16
		want   []byte
	}{
		{0, []byte{0x51, 0x83, 0xf3, 0x00, 0x00, 0x4f}},
		{0x0020, []byte{0x51, 0x83, 0xf3, 0x00, 0x20, 0x6f}},
		{0x1234, []byte{0x51, 0x83, 0xf3, 0x12, 0x34, 0x69}},
	}
	for _, tt := range tests {
		if got := EncodeCapabilitiesRequest(tt.offset); !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("offset=0x%04x got % x want % x", tt.offset, got, tt.want)
		}
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

func TestParseGetVCPReplyRetainsRawBytesOnFailure(t *testing.T) {
	reply := []byte{0x00, 0x60, 0x00}
	_, err := ParseGetVCPReply(reply, VCPInputSource)
	raw, ok := ReplyBytes(err)
	if !ok || !reflect.DeepEqual(raw, reply) {
		t.Fatalf("raw=% x ok=%v", raw, ok)
	}
	raw[0] = 0xff
	again, _ := ReplyBytes(err)
	if again[0] != 0x00 {
		t.Fatalf("ReplyBytes did not return a defensive copy: % x", again)
	}
}

func TestParseCapabilitiesReply(t *testing.T) {
	reply := validCapabilitiesReply(0x20, []byte("60(0F 11 12)"))
	fragment, err := ParseCapabilitiesReply(reply, 0x20)
	if err != nil {
		t.Fatal(err)
	}
	if fragment.Offset != 0x20 || string(fragment.Data) != "60(0F 11 12)" {
		t.Fatalf("unexpected fragment: %+v", fragment)
	}

	empty, err := ParseCapabilitiesReply(validCapabilitiesReply(0x2e, nil), 0x2e)
	if err != nil || empty.Offset != 0x2e || len(empty.Data) != 0 {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
}

func TestParseCapabilitiesReplyRejectsProtocolErrors(t *testing.T) {
	tests := []struct {
		name string
		edit func([]byte)
		want error
	}{
		{"source", func(reply []byte) { reply[0] = 0x50 }, monitor.ErrMalformedReply},
		{"length-marker", func(reply []byte) { reply[1] &^= 0x80 }, monitor.ErrMalformedReply},
		{"oversized", func(reply []byte) { reply[1] = 0xa4 }, monitor.ErrMalformedReply},
		{"command", func(reply []byte) { reply[2] = 0xe2 }, monitor.ErrMalformedReply},
		{"offset", func(reply []byte) { reply[4]++ }, monitor.ErrMalformedReply},
		{"checksum", func(reply []byte) { reply[len(reply)-1] ^= 0xff }, monitor.ErrChecksum},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reply := validCapabilitiesReply(0, []byte("abc"))
			tt.edit(reply)
			if tt.name != "checksum" && tt.name != "source" && tt.name != "length-marker" && tt.name != "oversized" {
				setReplyChecksum(reply)
			}
			if _, err := ParseCapabilitiesReply(reply, 0); !errors.Is(err, tt.want) {
				t.Fatalf("error=%v want %v; reply=% x", err, tt.want, reply)
			}
		})
	}
	if _, err := ParseCapabilitiesReply([]byte{0x6e, 0x80, 0xbe}, 0); !errors.Is(err, monitor.ErrCapabilitiesUnsupported) {
		t.Fatalf("null response error=%v", err)
	}
	if _, err := ParseCapabilitiesReply([]byte{0x6e, 0x80, 0x00}, 0); !errors.Is(err, monitor.ErrChecksum) {
		t.Fatalf("corrupt null response error=%v", err)
	}
	if _, err := ParseCapabilitiesReply(validCapabilitiesReply(0, []byte("abc"))[:5], 0); !errors.Is(err, monitor.ErrMalformedReply) {
		t.Fatalf("truncated error=%v", err)
	}
}

func FuzzParseCapabilitiesReply(f *testing.F) {
	f.Add(validCapabilitiesReply(0, []byte("(prot(monitor)")))
	f.Fuzz(func(t *testing.T, raw []byte) {
		_, _ = ParseCapabilitiesReply(raw, 0)
	})
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

func validCapabilitiesReply(offset uint16, data []byte) []byte {
	length := 3 + len(data)
	reply := make([]byte, 3+length)
	reply[0] = DisplayAddress
	reply[1] = 0x80 | byte(length)
	reply[2] = CapabilitiesReply
	reply[3] = byte(offset >> 8)
	reply[4] = byte(offset)
	copy(reply[5:], data)
	setReplyChecksum(reply)
	return reply
}
