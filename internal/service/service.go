package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
	"github.com/markz0r/XDispDDCSwtchr/internal/profiles"
)

type Sleeper interface {
	Sleep(context.Context, time.Duration) error
}

type Clock interface {
	Now() time.Time
}

type realSleeper struct{}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realSleeper) Sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type Service struct {
	backend  monitor.Backend
	registry *profiles.Registry
	matrix   *profiles.SupportMatrix
	platform profiles.Platform
	sleeper  Sleeper
	clock    Clock

	mu              sync.RWMutex
	snapshot        monitor.Snapshot
	locks           map[string]*sync.Mutex
	qualifiedInputs map[string]map[monitor.Input]bool
}

func New(backend monitor.Backend, registry *profiles.Registry, matrix *profiles.SupportMatrix, platform profiles.Platform) *Service {
	return &Service{
		backend: backend, registry: registry, matrix: matrix, platform: platform, sleeper: realSleeper{}, clock: realClock{},
		locks: map[string]*sync.Mutex{}, qualifiedInputs: map[string]map[monitor.Input]bool{},
	}
}

func (s *Service) SetSleeperForTest(sleeper Sleeper) { s.sleeper = sleeper }

func (s *Service) SetClockForTest(clock Clock) { s.clock = clock }

func (s *Service) Discover(ctx context.Context) (monitor.Snapshot, error) {
	snapshot, err := s.backend.Enumerate(ctx)
	if err != nil {
		return monitor.Snapshot{}, err
	}
	qualified := make(map[string]map[monitor.Input]bool)
	for i := range snapshot.Monitors {
		d := &snapshot.Monitors[i]
		profile, ok := s.registry.Match(d.Manufacturer, d.ProductCode, d.Model)
		if !ok {
			d.SupportState = monitor.SupportUnprofiled
			continue
		}
		d.ProfileName = profile.Name
		if record, ok := s.matrix.Match(s.platform, d.BackendName, profile.Name, d.EDIDSHA256, d.Connector); ok &&
			len(record.Inputs) > 0 && record.Verification == profile.Verification {
			allowed := make(map[monitor.Input]bool, len(record.Inputs))
			for _, value := range record.Inputs {
				logical := monitor.Input(value)
				if _, err := profile.Resolve(logical); err == nil {
					allowed[logical] = true
				}
			}
			if len(allowed) == 0 {
				d.SupportState = monitor.SupportBackendExperimental
				continue
			}
			qualified[d.ID] = allowed
			d.SupportState = monitor.SupportSupported
		} else {
			d.SupportState = monitor.SupportBackendExperimental
		}
	}
	s.mu.Lock()
	s.snapshot = snapshot
	s.qualifiedInputs = qualified
	s.mu.Unlock()
	return snapshot, nil
}

func (s *Service) Snapshot() monitor.Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return monitor.Snapshot{Generation: s.snapshot.Generation, Monitors: append([]monitor.Descriptor(nil), s.snapshot.Monitors...)}
}

func (s *Service) Inputs(ref monitor.MonitorRef) ([]monitor.InputValue, error) {
	d, err := s.descriptor(ref)
	if err != nil {
		return nil, err
	}
	if d.ProfileName == "" {
		return nil, monitor.ErrUnsupportedMonitor
	}
	if _, ok := s.registry.ByName(d.ProfileName); !ok {
		return nil, monitor.ErrUnsupportedMonitor
	}
	if d.SupportState != monitor.SupportSupported {
		return nil, monitor.ErrUnqualifiedSlice
	}
	values := s.registry.Inputs(d.ProfileName)
	s.mu.RLock()
	allowed := s.qualifiedInputs[d.ID]
	s.mu.RUnlock()
	out := values[:0]
	for _, value := range values {
		if allowed[value.Logical] {
			out = append(out, value)
		}
	}
	return out, nil
}

func (s *Service) GetInput(ctx context.Context, ref monitor.MonitorRef) (monitor.InputValue, error) {
	d, err := s.descriptor(ref)
	if err != nil {
		return monitor.InputValue{}, err
	}
	profile, ok := s.registry.ByName(d.ProfileName)
	if !ok || !profile.ReadSupported {
		return monitor.InputValue{}, monitor.ErrReadUnsupported
	}
	lock := s.lockFor(d.ID)
	lock.Lock()
	defer lock.Unlock()
	session, err := s.backend.Open(ctx, ref)
	if err != nil {
		return monitor.InputValue{}, err
	}
	defer session.Close()
	raw, _, err := s.readWithRetry(ctx, session, profile)
	if err != nil {
		return monitor.InputValue{}, err
	}
	if d.SupportState != monitor.SupportSupported {
		return monitor.InputValue{Raw: raw}, nil
	}
	value, known := profile.Logical(raw)
	if !known {
		return value, fmt.Errorf("%w: raw value 0x%02x", monitor.ErrUnknownInput, raw)
	}
	return value, nil
}

func (s *Service) Switch(ctx context.Context, ref monitor.MonitorRef, input monitor.Input) (monitor.SwitchResult, error) {
	d, err := s.descriptor(ref)
	if err != nil {
		return monitor.SwitchResult{}, err
	}
	if d.SupportState != monitor.SupportSupported {
		return monitor.SwitchResult{}, monitor.ErrUnqualifiedSlice
	}
	profile, ok := s.registry.ByName(d.ProfileName)
	if !ok {
		return monitor.SwitchResult{}, monitor.ErrUnsupportedMonitor
	}
	target, err := profile.Resolve(input)
	if err != nil {
		return monitor.SwitchResult{}, err
	}
	s.mu.RLock()
	qualified := s.qualifiedInputs[d.ID][input]
	s.mu.RUnlock()
	if !qualified {
		return monitor.SwitchResult{}, monitor.ErrUnqualifiedSlice
	}

	started := s.clock.Now()
	result := monitor.SwitchResult{Monitor: ref, Target: target, Attempts: 1}
	lock := s.lockFor(d.ID)
	lock.Lock()
	defer lock.Unlock()
	session, err := s.backend.Open(ctx, ref)
	if err != nil {
		return result, err
	}
	defer session.Close()
	for attempt := 1; ; attempt++ {
		result.Attempts = attempt
		err = session.SetInputRaw(ctx, target.Raw)
		if err == nil {
			break
		}
		if attempt > profile.RetryCount || !retryableWrite(err) {
			return result, err
		}
		if err := s.sleeper.Sleep(ctx, profile.ReplyDelay); err != nil {
			return result, err
		}
	}
	result.Verification = monitor.VerificationWriteAccepted
	if err := s.sleeper.Sleep(ctx, profile.WriteDelay); err != nil {
		return result, err
	}
	if profile.Verification == profiles.VerifyNone || !profile.ReadSupported {
		result.Duration = s.clock.Now().Sub(started)
		return result, nil
	}
	raw, _, err := s.readWithRetry(ctx, session, profile)
	if err != nil {
		if errors.Is(err, monitor.ErrDisconnected) || errors.Is(err, monitor.ErrEndpointNotFound) {
			result.Verification = monitor.VerificationDisplayPathChanged
			result.Duration = s.clock.Now().Sub(started)
			return result, nil
		}
		return result, err
	}
	observed, _ := profile.Logical(raw)
	result.Observed = &observed
	result.Duration = s.clock.Now().Sub(started)
	if raw != target.Raw {
		result.Verification = monitor.VerificationUnknown
		return result, fmt.Errorf("verification observed raw 0x%02x, expected 0x%02x", raw, target.Raw)
	}
	result.Verification = monitor.VerificationConfirmed
	return result, nil
}

func (s *Service) readWithRetry(ctx context.Context, session monitor.RawInputSession, profile *profiles.Profile) (uint16, int, error) {
	for attempt := 1; ; attempt++ {
		raw, err := session.GetInputRaw(ctx)
		if err == nil {
			return raw, attempt, nil
		}
		if attempt > profile.RetryCount || !retryableRead(err) {
			return 0, attempt, err
		}
		if err := s.sleeper.Sleep(ctx, profile.ReplyDelay); err != nil {
			return 0, attempt, err
		}
	}
}

func retryableRead(err error) bool {
	return errors.Is(err, monitor.ErrTransactionNACK) ||
		errors.Is(err, monitor.ErrTransactionTimeout) ||
		errors.Is(err, monitor.ErrMalformedReply) ||
		errors.Is(err, monitor.ErrChecksum)
}

func retryableWrite(err error) bool {
	// A NACK proves the monitor did not accept the write. A timeout or
	// disconnect is ambiguous and must not cause an automatic duplicate write.
	return errors.Is(err, monitor.ErrTransactionNACK)
}

func (s *Service) descriptor(ref monitor.MonitorRef) (monitor.Descriptor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ref.Generation != s.snapshot.Generation {
		return monitor.Descriptor{}, monitor.ErrStaleEnumeration
	}
	for _, d := range s.snapshot.Monitors {
		if d.ID == ref.ID {
			return d, nil
		}
	}
	return monitor.Descriptor{}, monitor.ErrMonitorNotFound
}

func (s *Service) lockFor(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locks[id] == nil {
		s.locks[id] = &sync.Mutex{}
	}
	return s.locks[id]
}
