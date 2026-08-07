package capabilities

import (
	"fmt"
	"strings"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

const MaxStringLength = 16 * 1024

type Result struct {
	InputSourceAdvertised bool
	InputValues           []uint16
}

// Parse extracts only the VCP 0x60 discrete values needed by this
// application. Other capability segments and VCP features are intentionally
// ignored.
func Parse(raw string) (Result, error) {
	if len(raw) > MaxStringLength {
		return Result{}, fmt.Errorf("%w: capability string is %d bytes, limit is %d", monitor.ErrMalformedReply, len(raw), MaxStringLength)
	}
	body, found, err := namedGroup(raw, "vcp")
	if err != nil {
		return Result{}, err
	}
	if !found {
		return Result{}, nil
	}
	return parseVCP(body)
}

func StandardInput(value uint16) (monitor.Input, bool) {
	switch value {
	case 0x0f:
		return monitor.InputDisplayPort1, true
	case 0x10:
		return monitor.InputDisplayPort2, true
	case 0x11:
		return monitor.InputHDMI1, true
	case 0x12:
		return monitor.InputHDMI2, true
	default:
		return "", false
	}
}

func namedGroup(raw, name string) (string, bool, error) {
	for index := 0; index < len(raw); {
		if !isLetter(raw[index]) {
			index++
			continue
		}
		start := index
		for index < len(raw) && (isLetter(raw[index]) || raw[index] == '_') {
			index++
		}
		identifier := raw[start:index]
		for index < len(raw) && isSpace(raw[index]) {
			index++
		}
		if !strings.EqualFold(identifier, name) || index >= len(raw) || raw[index] != '(' {
			continue
		}
		end, err := closingParen(raw, index)
		if err != nil {
			return "", false, err
		}
		return raw[index+1 : end], true, nil
	}
	return "", false, nil
}

func closingParen(raw string, opening int) (int, error) {
	depth := 0
	for index := opening; index < len(raw); index++ {
		switch raw[index] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return index, nil
			}
		}
	}
	return 0, fmt.Errorf("%w: unterminated capability group", monitor.ErrMalformedReply)
}

func parseVCP(body string) (Result, error) {
	result := Result{InputValues: []uint16{}}
	seen := make(map[uint16]bool)
	depth := 0
	var feature uint16
	for index := 0; index < len(body); {
		if isSpace(body[index]) {
			index++
			continue
		}
		switch body[index] {
		case '(':
			depth++
			index++
			continue
		case ')':
			if depth == 0 {
				return Result{}, fmt.Errorf("%w: unexpected ')' in vcp group", monitor.ErrMalformedReply)
			}
			depth--
			index++
			continue
		}
		if index+2 > len(body) || !isHex(body[index]) || !isHex(body[index+1]) {
			return Result{}, fmt.Errorf("%w: invalid vcp token at byte %d", monitor.ErrMalformedReply, index)
		}
		value := uint16(hexValue(body[index])<<4 | hexValue(body[index+1]))
		index += 2
		if depth == 0 {
			feature = value
			if feature == 0x60 {
				result.InputSourceAdvertised = true
			}
			continue
		}
		if depth == 1 && feature == 0x60 && !seen[value] {
			seen[value] = true
			result.InputValues = append(result.InputValues, value)
		}
	}
	if depth != 0 {
		return Result{}, fmt.Errorf("%w: unterminated nested vcp value group", monitor.ErrMalformedReply)
	}
	return result, nil
}

func isLetter(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isSpace(value byte) bool {
	switch value {
	case ' ', '\t', '\r', '\n', '\x00':
		return true
	default:
		return false
	}
}

func isHex(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}

func hexValue(value byte) byte {
	switch {
	case value >= '0' && value <= '9':
		return value - '0'
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10
	default:
		return value - 'A' + 10
	}
}
