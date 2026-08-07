//go:build darwin && arm64

package darwin

/*
#cgo LDFLAGS: -framework CoreFoundation -framework CoreGraphics -framework IOKit
#include "bridge_darwin.h"
*/
import "C"

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/markz0r/XDispDDCSwtchr/internal/ddc"
	"github.com/markz0r/XDispDDCSwtchr/internal/edid"
	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

const name = "macos-native"

type endpoint struct {
	registryID uint64
}

type Backend struct {
	mu         sync.RWMutex
	generation monitor.Generation
	endpoints  map[string]endpoint
}

func New() (*Backend, error) {
	var message [C.XDISP_MAX_ERROR]C.char
	if status := C.xdispddcswtchr_symbol_probe(&message[0]); status != 0 {
		return nil, nativeError(int(status), C.GoString(&message[0]))
	}
	return &Backend{endpoints: make(map[string]endpoint)}, nil
}

func (b *Backend) Name() string { return name }

func (b *Backend) Enumerate(ctx context.Context) (monitor.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return monitor.Snapshot{}, err
	}
	var count C.uint32_t
	var message [C.XDISP_MAX_ERROR]C.char
	if status := C.xdispddcswtchr_display_count(&count, &message[0]); status != 0 {
		return monitor.Snapshot{}, nativeError(int(status), C.GoString(&message[0]))
	}
	if count == 0 {
		return monitor.Snapshot{}, monitor.ErrNoMonitors
	}

	type candidate struct {
		info       edid.Info
		raw        []byte
		connector  string
		registryID uint64
	}
	candidates := make([]candidate, 0, int(count))
	serialCounts := make(map[string]int)
	for i := C.uint32_t(0); i < count; i++ {
		if err := ctx.Err(); err != nil {
			return monitor.Snapshot{}, err
		}
		var identity C.xdisp_display_info
		message = [C.XDISP_MAX_ERROR]C.char{}
		if status := C.xdispddcswtchr_display_identity(i, &identity, &message[0]); status != 0 {
			return monitor.Snapshot{}, nativeError(int(status), C.GoString(&message[0]))
		}
		length := int(identity.edid_length)
		if length < edid.BlockSize || length > C.XDISP_MAX_EDID {
			return monitor.Snapshot{}, fmt.Errorf("%w: native backend returned %d EDID bytes", monitor.ErrEndpointNotFound, length)
		}
		raw := C.GoBytes(unsafe.Pointer(&identity.edid[0]), C.int(length))
		parsed, err := edid.Parse(raw)
		if err != nil {
			return monitor.Snapshot{}, fmt.Errorf("parse display %d EDID: %w", uint32(identity.display_id), err)
		}
		serialKey := fmt.Sprintf("%s:%04x:%08x", parsed.Manufacturer, parsed.ProductCode, parsed.NumericSerial)
		if parsed.NumericSerial != 0 {
			serialCounts[serialKey]++
		}
		candidates = append(candidates, candidate{
			info:       parsed,
			raw:        raw,
			connector:  connectorName(C.GoString(&identity.service_path[0])),
			registryID: uint64(identity.service_registry_id),
		})
	}

	monitors := make([]monitor.Descriptor, 0, len(candidates))
	endpoints := make(map[string]endpoint, len(candidates))
	for _, candidate := range candidates {
		serialKey := fmt.Sprintf("%s:%04x:%08x", candidate.info.Manufacturer, candidate.info.ProductCode, candidate.info.NumericSerial)
		id, state, err := edid.StableID(candidate.info, candidate.connector, candidate.info.NumericSerial != 0 && serialCounts[serialKey] == 1)
		if err != nil {
			return monitor.Snapshot{}, err
		}
		if _, duplicate := endpoints[id]; duplicate {
			return monitor.Snapshot{}, fmt.Errorf("%w: duplicate stable monitor ID %s", monitor.ErrAmbiguousMonitor, id)
		}
		serial := candidate.info.TextSerial
		if serial == "" && candidate.info.NumericSerial != 0 {
			serial = fmt.Sprintf("%08x", candidate.info.NumericSerial)
		}
		monitors = append(monitors, monitor.Descriptor{
			ID:            id,
			Manufacturer:  candidate.info.Manufacturer,
			Model:         candidate.info.ModelName,
			Serial:        serial,
			ProductCode:   candidate.info.ProductCode,
			Connector:     candidate.connector,
			EDID:          append([]byte(nil), candidate.raw...),
			EDIDSHA256:    edid.SHA256(candidate.raw),
			IdentityState: state,
			SupportState:  monitor.SupportBackendExperimental,
			BackendName:   name,
		})
		endpoints[id] = endpoint{registryID: candidate.registryID}
	}

	b.mu.Lock()
	b.generation++
	generation := b.generation
	b.endpoints = endpoints
	b.mu.Unlock()
	return monitor.Snapshot{Generation: generation, Monitors: monitors}, nil
}

func (b *Backend) Open(ctx context.Context, ref monitor.MonitorRef) (monitor.RawInputSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.RLock()
	generation := b.generation
	ep, ok := b.endpoints[ref.ID]
	b.mu.RUnlock()
	if ref.Generation != generation {
		return nil, monitor.ErrStaleEnumeration
	}
	if !ok {
		return nil, monitor.ErrMonitorNotFound
	}
	var handle C.xdisp_ddc_handle
	var message [C.XDISP_MAX_ERROR]C.char
	if status := C.xdispddcswtchr_ddc_open(C.uint64_t(ep.registryID), &handle, &message[0]); status != 0 {
		return nil, nativeError(int(status), C.GoString(&message[0]))
	}
	return &session{handle: handle, replyDelay: 50 * time.Millisecond}, nil
}

type session struct {
	mu         sync.Mutex
	handle     C.xdisp_ddc_handle
	replyDelay time.Duration
	closed     bool
}

func (s *session) GetInputRaw(ctx context.Context) (uint16, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return 0, err
	}
	request := ddc.EncodeGetVCP(ddc.VCPInputSource)
	reply := make([]byte, 11)
	if err := transaction(s.handle, request, reply, s.replyDelay); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	parsed, err := parseCoreDisplayGetVCPReply(reply, ddc.VCPInputSource)
	if err != nil {
		return 0, err
	}
	return ddc.NormalizeInputSourceValue(parsed.Current), nil
}

// parseCoreDisplayGetVCPReply accepts the standard MCCS frame first. Some
// Apple Silicon CoreDisplay transports return only the eight bytes beginning
// at the DDC result field, leaving the fixed 11-byte read buffer's final three
// bytes zeroed. Reconstruct only that exact representation, then delegate all
// length, command, result, code, and checksum validation to the strict parser.
func parseCoreDisplayGetVCPReply(reply []byte, expectedCode byte) (ddc.VCPReply, error) {
	parsed, standardErr := ddc.ParseGetVCPReply(reply, expectedCode)
	if standardErr == nil {
		return parsed, nil
	}
	if len(reply) != 11 || reply[1] != expectedCode || reply[8] != 0 || reply[9] != 0 || reply[10] != 0 {
		return ddc.VCPReply{}, standardErr
	}
	canonical := make([]byte, 0, 11)
	canonical = append(canonical, ddc.DisplayAddress, 0x88, 0x02)
	canonical = append(canonical, reply[:8]...)
	parsed, compactErr := ddc.ParseGetVCPReply(canonical, expectedCode)
	if compactErr != nil {
		return ddc.VCPReply{}, ddc.WithReplyBytes(compactErr, reply)
	}
	return parsed, nil
}

func (s *session) SetInputRaw(ctx context.Context, value uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return err
	}
	if err := transaction(s.handle, ddc.EncodeSetVCP(ddc.VCPInputSource, value), nil, 0); err != nil {
		return err
	}
	return ctx.Err()
}

func (s *session) Capabilities(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.check(ctx); err != nil {
		return "", err
	}

	assembled := make([]byte, 0, 2048)
	for {
		if len(assembled) > ddc.MaxCapabilitiesLength {
			return "", fmt.Errorf("%w: capability string exceeds %d bytes", monitor.ErrMalformedReply, ddc.MaxCapabilitiesLength)
		}
		offset := uint16(len(assembled))
		reply := make([]byte, ddc.MaxCapabilitiesReplySize)
		if err := transaction(s.handle, ddc.EncodeCapabilitiesRequest(offset), reply, s.replyDelay); err != nil {
			return "", err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		fragment, err := ddc.ParseCapabilitiesReply(reply, offset)
		if err != nil {
			return "", err
		}
		if len(fragment.Data) == 0 {
			break
		}
		if len(assembled)+len(fragment.Data) > ddc.MaxCapabilitiesLength || len(assembled)+len(fragment.Data) > int(^uint16(0)) {
			return "", fmt.Errorf("%w: capability string exceeds safe protocol limit", monitor.ErrMalformedReply)
		}
		assembled = append(assembled, fragment.Data...)
		if err := waitContext(ctx, s.replyDelay); err != nil {
			return "", err
		}
	}
	return strings.TrimRight(string(assembled), " \t\r\n\x00"), nil
}

func (s *session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	C.xdispddcswtchr_ddc_close(s.handle)
	s.handle = nil
	return nil
}

func (s *session) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed || s.handle == nil {
		return monitor.ErrDisconnected
	}
	return nil
}

func transaction(handle C.xdisp_ddc_handle, request, reply []byte, delay time.Duration) error {
	var replyPointer *C.uint8_t
	if len(reply) > 0 {
		replyPointer = (*C.uint8_t)(unsafe.Pointer(&reply[0]))
	}
	var nativeStatus C.int32_t
	var message [C.XDISP_MAX_ERROR]C.char
	status := C.xdispddcswtchr_ddc_transaction(
		handle,
		(*C.uint8_t)(unsafe.Pointer(&request[0])),
		C.uint32_t(len(request)),
		replyPointer,
		C.uint32_t(len(reply)),
		C.uint32_t(delay.Microseconds()),
		&nativeStatus,
		&message[0],
	)
	if status != 0 {
		return nativeError(int(status), C.GoString(&message[0]))
	}
	return nil
}

func waitContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func connectorName(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.Contains(lower, "/dcpext0@"):
		return "external-dcpext0"
	case strings.Contains(lower, "/dcp@"):
		return "external-dcp"
	default:
		return "external"
	}
}

func nativeError(status int, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		message = fmt.Sprintf("native status %d", status)
	}
	var kind error
	switch status {
	case -2:
		kind = monitor.ErrPrivateSymbolUnavailable
	case -3, -5:
		kind = monitor.ErrBackendUnavailable
	case -6, -7:
		kind = monitor.ErrEndpointNotFound
	case -8:
		kind = monitor.ErrDisconnected
	case -9:
		kind = monitor.ErrAmbiguousMonitor
	default:
		kind = monitor.ErrBackendUnavailable
	}
	return fmt.Errorf("%w: %s", kind, message)
}

func discoveryProbe() (string, error) {
	var output [C.XDISP_MAX_DIAGNOSTIC]C.char
	var message [C.XDISP_MAX_ERROR]C.char
	if status := C.xdispddcswtchr_discovery_probe(&output[0], &message[0]); status != 0 {
		return "", nativeError(int(status), C.GoString(&message[0]))
	}
	return C.GoString(&output[0]), nil
}

var _ monitor.Backend = (*Backend)(nil)
var _ monitor.RawInputSession = (*session)(nil)
var _ monitor.CapabilitySession = (*session)(nil)
