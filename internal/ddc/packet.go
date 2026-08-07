package ddc

import (
	"errors"
	"fmt"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

const (
	VCPInputSource           byte = 0x60
	CapabilitiesRequest      byte = 0xf3
	CapabilitiesReply        byte = 0xe3
	HostAddress              byte = 0x51
	DisplayAddress           byte = 0x6e
	DisplayReadByte          byte = 0x6f
	MaxCapabilitiesFragment       = 32
	MaxCapabilitiesReplySize      = 39
	MaxCapabilitiesLength         = 16 * 1024
)

func EncodeGetVCP(code byte) []byte {
	return encode([]byte{0x01, code})
}

func EncodeSetVCP(code byte, value uint16) []byte {
	return encode([]byte{0x03, code, byte(value >> 8), byte(value)})
}

func EncodeCapabilitiesRequest(offset uint16) []byte {
	return encode([]byte{CapabilitiesRequest, byte(offset >> 8), byte(offset)})
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

type CapabilitiesFragment struct {
	Offset uint16
	Data   []byte
}

// ReplyError retains the exact bytes returned by the transport while preserving
// errors.Is classification through Cause. It lets higher layers report useful
// diagnostics without weakening the protocol parser.
type ReplyError struct {
	Raw   []byte
	Cause error
}

func (e *ReplyError) Error() string { return e.Cause.Error() }
func (e *ReplyError) Unwrap() error { return e.Cause }

// ReplyBytes returns a defensive copy of raw transport bytes carried by err.
func ReplyBytes(err error) ([]byte, bool) {
	var replyErr *ReplyError
	if !errors.As(err, &replyErr) {
		return nil, false
	}
	return append([]byte(nil), replyErr.Raw...), true
}

func newReplyError(reply []byte, cause error) error {
	return &ReplyError{Raw: append([]byte(nil), reply...), Cause: cause}
}

// WithReplyBytes attaches the transport's original representation to a
// protocol error produced after boundary-specific normalization.
func WithReplyBytes(err error, reply []byte) error {
	if err == nil {
		return nil
	}
	return newReplyError(reply, err)
}

func ParseCapabilitiesReply(reply []byte, expectedOffset uint16) (CapabilitiesFragment, error) {
	if len(reply) < 3 {
		return CapabilitiesFragment{}, fmt.Errorf("%w: got %d bytes, need at least 3", monitor.ErrMalformedReply, len(reply))
	}
	if reply[0] != DisplayAddress {
		return CapabilitiesFragment{}, fmt.Errorf("%w: unexpected source address 0x%02x", monitor.ErrMalformedReply, reply[0])
	}
	if reply[1]&0x80 == 0 {
		return CapabilitiesFragment{}, fmt.Errorf("%w: capability reply length marker is clear", monitor.ErrMalformedReply)
	}
	length := int(reply[1] & 0x7f)
	if length == 0 {
		wantChecksum := checksum(DisplayReadByte, []byte{HostAddress, reply[1]})
		if reply[2] != wantChecksum {
			return CapabilitiesFragment{}, fmt.Errorf("%w: got 0x%02x want 0x%02x", monitor.ErrChecksum, reply[2], wantChecksum)
		}
		return CapabilitiesFragment{}, monitor.ErrCapabilitiesUnsupported
	}
	if length < 3 || length > 3+MaxCapabilitiesFragment {
		return CapabilitiesFragment{}, fmt.Errorf("%w: invalid capability payload length %d", monitor.ErrMalformedReply, length)
	}
	checksumIndex := 2 + length
	if checksumIndex >= len(reply) {
		return CapabilitiesFragment{}, fmt.Errorf("%w: declared capability payload length %d exceeds %d-byte reply", monitor.ErrMalformedReply, length, len(reply))
	}
	wantChecksum := checksum(DisplayReadByte, append([]byte{HostAddress}, reply[1:checksumIndex]...))
	if reply[checksumIndex] != wantChecksum {
		return CapabilitiesFragment{}, fmt.Errorf("%w: got 0x%02x want 0x%02x", monitor.ErrChecksum, reply[checksumIndex], wantChecksum)
	}
	if reply[2] != CapabilitiesReply {
		return CapabilitiesFragment{}, fmt.Errorf("%w: unexpected command 0x%02x", monitor.ErrMalformedReply, reply[2])
	}
	offset := uint16(reply[3])<<8 | uint16(reply[4])
	if offset != expectedOffset {
		return CapabilitiesFragment{}, fmt.Errorf("%w: capability fragment offset %d, expected %d", monitor.ErrMalformedReply, offset, expectedOffset)
	}
	data := append([]byte(nil), reply[5:checksumIndex]...)
	return CapabilitiesFragment{Offset: offset, Data: data}, nil
}

func ParseGetVCPReply(reply []byte, expectedCode byte) (VCPReply, error) {
	if len(reply) < 11 {
		return VCPReply{}, newReplyError(reply, fmt.Errorf("%w: got %d bytes, need at least 11", monitor.ErrMalformedReply, len(reply)))
	}
	length := int(reply[1] & 0x7f)
	if length != 8 || 2+length >= len(reply) {
		return VCPReply{}, newReplyError(reply, fmt.Errorf("%w: invalid payload length %d", monitor.ErrMalformedReply, length))
	}
	wantChecksum := checksum(DisplayReadByte, append([]byte{HostAddress}, reply[1:2+length]...))
	if reply[2+length] != wantChecksum {
		return VCPReply{}, newReplyError(reply, fmt.Errorf("%w: got 0x%02x want 0x%02x", monitor.ErrChecksum, reply[2+length], wantChecksum))
	}
	if reply[2] != 0x02 {
		return VCPReply{}, newReplyError(reply, fmt.Errorf("%w: unexpected command 0x%02x", monitor.ErrMalformedReply, reply[2]))
	}
	if reply[3] != 0 {
		return VCPReply{}, newReplyError(reply, fmt.Errorf("%w: monitor result 0x%02x", monitor.ErrReadUnsupported, reply[3]))
	}
	if reply[4] != expectedCode {
		return VCPReply{}, newReplyError(reply, fmt.Errorf("%w: reply code 0x%02x, expected 0x%02x", monitor.ErrMalformedReply, reply[4], expectedCode))
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
