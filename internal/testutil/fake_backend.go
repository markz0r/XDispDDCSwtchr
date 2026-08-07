package testutil

import (
	"context"
	"fmt"
	"sync"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

type FakeBackend struct {
	mu                sync.Mutex
	Generation        monitor.Generation
	Descriptors       []monitor.Descriptor
	Inputs            map[string]uint16
	Capabilities      map[string]string
	EnumerateError    error
	OpenError         error
	GetError          error
	CapabilitiesError error
	SetError          error
	GetErrors         []error
	SetErrors         []error
	GetCalls          int
	CapabilitiesCalls int
	SetCalls          []SetCall
	IgnoreSets        bool
	OperationStarted  chan string
	OperationRelease  <-chan struct{}
	active            int
	MaxActive         int
}

type SetCall struct {
	MonitorID string
	Value     uint16
}

func (f *FakeBackend) Name() string { return "fake" }

func (f *FakeBackend) Enumerate(ctx context.Context) (monitor.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return monitor.Snapshot{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.EnumerateError != nil {
		return monitor.Snapshot{}, f.EnumerateError
	}
	f.Generation++
	descriptors := append([]monitor.Descriptor(nil), f.Descriptors...)
	return monitor.Snapshot{Generation: f.Generation, Monitors: descriptors}, nil
}

func (f *FakeBackend) Open(ctx context.Context, ref monitor.MonitorRef) (monitor.RawInputSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.OpenError != nil {
		return nil, f.OpenError
	}
	if ref.Generation != f.Generation {
		return nil, monitor.ErrStaleEnumeration
	}
	for _, d := range f.Descriptors {
		if d.ID == ref.ID {
			return &fakeSession{backend: f, id: ref.ID}, nil
		}
	}
	return nil, monitor.ErrMonitorNotFound
}

type fakeSession struct {
	backend *FakeBackend
	id      string
	closed  bool
}

func (s *fakeSession) GetInputRaw(ctx context.Context) (uint16, error) {
	if err := s.begin(ctx); err != nil {
		return 0, err
	}
	defer s.end()
	if err := s.wait(ctx); err != nil {
		return 0, err
	}
	s.backend.mu.Lock()
	s.backend.GetCalls++
	var err error
	if len(s.backend.GetErrors) > 0 {
		err = s.backend.GetErrors[0]
		s.backend.GetErrors = s.backend.GetErrors[1:]
	} else {
		err = s.backend.GetError
	}
	value, ok := s.backend.Inputs[s.id]
	s.backend.mu.Unlock()
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("%w: no fake value", monitor.ErrReadUnsupported)
	}
	return value, nil
}

func (s *fakeSession) SetInputRaw(ctx context.Context, value uint16) error {
	if err := s.begin(ctx); err != nil {
		return err
	}
	defer s.end()
	if err := s.wait(ctx); err != nil {
		return err
	}
	s.backend.mu.Lock()
	var err error
	if len(s.backend.SetErrors) > 0 {
		err = s.backend.SetErrors[0]
		s.backend.SetErrors = s.backend.SetErrors[1:]
	} else {
		err = s.backend.SetError
	}
	if err != nil {
		s.backend.mu.Unlock()
		return err
	}
	if !s.backend.IgnoreSets {
		s.backend.Inputs[s.id] = value
	}
	s.backend.SetCalls = append(s.backend.SetCalls, SetCall{MonitorID: s.id, Value: value})
	s.backend.mu.Unlock()
	return nil
}

func (s *fakeSession) Capabilities(ctx context.Context) (string, error) {
	if err := s.begin(ctx); err != nil {
		return "", err
	}
	defer s.end()
	if err := s.wait(ctx); err != nil {
		return "", err
	}
	s.backend.mu.Lock()
	defer s.backend.mu.Unlock()
	s.backend.CapabilitiesCalls++
	if s.backend.CapabilitiesError != nil {
		return "", s.backend.CapabilitiesError
	}
	value, ok := s.backend.Capabilities[s.id]
	if !ok {
		return "", monitor.ErrCapabilitiesUnsupported
	}
	return value, nil
}

func (s *fakeSession) Close() error {
	s.backend.mu.Lock()
	defer s.backend.mu.Unlock()
	s.closed = true
	return nil
}

func (s *fakeSession) begin(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.backend.mu.Lock()
	defer s.backend.mu.Unlock()
	if s.closed {
		return monitor.ErrDisconnected
	}
	s.backend.active++
	if s.backend.active > s.backend.MaxActive {
		s.backend.MaxActive = s.backend.active
	}
	return nil
}

func (s *fakeSession) end() {
	s.backend.mu.Lock()
	s.backend.active--
	s.backend.mu.Unlock()
}

func (s *fakeSession) wait(ctx context.Context) error {
	s.backend.mu.Lock()
	started := s.backend.OperationStarted
	release := s.backend.OperationRelease
	s.backend.mu.Unlock()
	if started != nil {
		select {
		case started <- s.id:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
