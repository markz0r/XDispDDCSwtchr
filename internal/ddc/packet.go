package ddc

import (
	"fmt"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

const (
	VCPInputSource  byte = 0x60
	HostAddress     byte = 0x51
	DisplayAddress  byte = 0x6e
	DisplayReadByte byte = 0x6f
)

func EncodeGetVCP(code byte) []byte {
	return encode([]byte{0x01, code})
}

func EncodeSetVCP(code byte, value uint16) []byte {
	return encode([]byte{0x03, code, byte(value >> 8), byte(value)})
}

func encode(payload []byte) []byte {
	packet := make([]byte, 0, len(payload)+3)
	packet = append(packet, HostAddress, 0x80|byte(len(payload)))
	packet = append(packet, payload...)
	packet = append(packet, checksum(DisplayAddress, packet))
	return packet
}

type VCPReply struct {
	Code       byte
	Continuous bool
	Maximum    uint16
	Current    uint16
}

func ParseGetVCPReply(reply []byte, expectedCode byte) (VCPReply, error) {
	if len(reply) < 11 {
		return VCPReply{}, fmt.Errorf("%w: got %d bytes, need at least 11", monitor.ErrMalformedReply, len(reply))
	}
	length := int(reply[1] & 0x7f)
	if length != 8 || 2+length >= len(reply) {
		return VCPReply{}, fmt.Errorf("%w: invalid payload length %d", monitor.ErrMalformedReply, length)
	}
	wantChecksum := checksum(DisplayReadByte, append([]byte{HostAddress}, reply[1:2+length]...))
	if reply[2+length] != wantChecksum {
		return VCPReply{}, fmt.Errorf("%w: got 0x%02x want 0x%02x", monitor.ErrChecksum, reply[2+length], wantChecksum)
	}
	if reply[2] != 0x02 {
		return VCPReply{}, fmt.Errorf("%w: unexpected command 0x%02x", monitor.ErrMalformedReply, reply[2])
	}
	if reply[3] != 0 {
		return VCPReply{}, fmt.Errorf("%w: monitor result 0x%02x", monitor.ErrReadUnsupported, reply[3])
	}
	if reply[4] != expectedCode {
		return VCPReply{}, fmt.Errorf("%w: reply code 0x%02x, expected 0x%02x", monitor.ErrMalformedReply, reply[4], expectedCode)
	}
	return VCPReply{
		Code:       reply[4],
		Continuous: reply[5] == 0,
		Maximum:    uint16(reply[6])<<8 | uint16(reply[7]),
		Current:    uint16(reply[8])<<8 | uint16(reply[9]),
	}, nil
}

// NormalizeInputSourceValue tolerates monitors that duplicate the one-byte
// table value into both bytes of the MCCS current-value word (for example,
// 0x1111 for HDMI-1). VCP 0x60 values are one-byte table entries.
func NormalizeInputSourceValue(value uint16) uint16 {
	high, low := byte(value>>8), byte(value)
	if high != 0 && high == low {
		return uint16(low)
	}
	return value
}

func checksum(seed byte, data []byte) byte {
	value := seed
	for _, b := range data {
		value ^= b
	}
	return value
}
