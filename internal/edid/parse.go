package edid

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

const BlockSize = 128

var header = []byte{0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00}

var (
	ErrTruncated       = errors.New("EDID base block is truncated")
	ErrInvalidHeader   = errors.New("EDID header is invalid")
	ErrInvalidChecksum = errors.New("EDID base checksum is invalid")
)

type ExtensionStatus struct {
	Index         int  `json:"index"`
	Present       bool `json:"present"`
	ChecksumValid bool `json:"checksum_valid"`
}

type Info struct {
	Manufacturer  string            `json:"manufacturer"`
	ProductCode   uint16            `json:"product_code"`
	NumericSerial uint32            `json:"numeric_serial"`
	TextSerial    string            `json:"text_serial,omitempty"`
	ModelName     string            `json:"model_name,omitempty"`
	Version       uint8             `json:"version"`
	Revision      uint8             `json:"revision"`
	Extensions    []ExtensionStatus `json:"extensions,omitempty"`
	Raw           []byte            `json:"-"`
}

func Parse(raw []byte) (Info, error) {
	if len(raw) < BlockSize {
		return Info{}, ErrTruncated
	}
	base := raw[:BlockSize]
	if !bytes.Equal(base[:len(header)], header) {
		return Info{}, ErrInvalidHeader
	}
	if !ChecksumValid(base) {
		return Info{}, ErrInvalidChecksum
	}

	info := Info{
		Manufacturer:  decodeManufacturer(binary.BigEndian.Uint16(base[8:10])),
		ProductCode:   binary.LittleEndian.Uint16(base[10:12]),
		NumericSerial: binary.LittleEndian.Uint32(base[12:16]),
		Version:       base[18],
		Revision:      base[19],
		Raw:           append([]byte(nil), raw...),
	}
	for offset := 54; offset+18 <= 126; offset += 18 {
		d := base[offset : offset+18]
		if d[0] != 0 || d[1] != 0 || d[2] != 0 {
			continue
		}
		value := cleanDescriptor(d[5:18])
		switch d[3] {
		case 0xfc:
			info.ModelName = value
		case 0xff:
			info.TextSerial = value
		}
	}

	declared := int(base[126])
	info.Extensions = make([]ExtensionStatus, declared)
	for i := 0; i < declared; i++ {
		start := (i + 1) * BlockSize
		status := ExtensionStatus{Index: i + 1}
		if start+BlockSize <= len(raw) {
			status.Present = true
			status.ChecksumValid = ChecksumValid(raw[start : start+BlockSize])
		}
		info.Extensions[i] = status
	}
	return info, nil
}

func ChecksumValid(block []byte) bool {
	if len(block) != BlockSize {
		return false
	}
	var sum byte
	for _, b := range block {
		sum += b
	}
	return sum == 0
}

func decodeManufacturer(encoded uint16) string {
	letters := [3]byte{
		byte((encoded>>10)&0x1f) + 'A' - 1,
		byte((encoded>>5)&0x1f) + 'A' - 1,
		byte(encoded&0x1f) + 'A' - 1,
	}
	for _, b := range letters {
		if b < 'A' || b > 'Z' {
			return ""
		}
	}
	return string(letters[:])
}

func cleanDescriptor(raw []byte) string {
	s := strings.TrimSpace(strings.TrimRight(string(raw), "\x00\n\r "))
	var out strings.Builder
	for _, r := range s {
		if r >= 0x20 && r <= 0x7e {
			out.WriteRune(r)
		}
	}
	return strings.TrimSpace(out.String())
}

func (i Info) ValidateExtensions() error {
	for _, ext := range i.Extensions {
		if !ext.Present {
			return fmt.Errorf("EDID extension %d is truncated", ext.Index)
		}
		if !ext.ChecksumValid {
			return fmt.Errorf("EDID extension %d checksum is invalid", ext.Index)
		}
	}
	return nil
}
