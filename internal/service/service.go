package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/markz0r/XDispDDCSwtchr/internal/capabilities"
	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
	"github.com/markz0r/XDispDDCSwtchr/internal/profiles"
	"github.com/markz0r/XDispDDCSwtchr/internal/qualification"
	"github.com/markz0r/XDispDDCSwtchr/internal/verification"
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

	mu                 sync.RWMutex
	snapshot           monitor.Snapshot
	locks              map[string]*sync.Mutex
	qualifiedInputs    map[string]map[monitor.Input]bool
	effectiveProfiles  map[string]*profiles.Profile
	userQualifications map[string]qualification.Record
	qualificationSaver func(qualification.Record) error
}

func New(backend monitor.Backend, registry *profiles.Registry, matrix *profiles.SupportMatrix, platform profiles.Platform) *Service {
	return &Service{
		backend: backend, registry: registry, matrix: matrix, platform: platform, sleeper: realSleeper{}, clock: realClock{},
		locks: map[string]*sync.Mutex{}, qualifiedInputs: map[string]map[monitor.Input]bool{},
		effectiveProfiles: map[string]*profiles.Profile{}, userQualifications: map[string]qualification.Record{},
	}
}

func (s *Service) SetSleeperForTest(sleeper Sleeper) { s.sleeper = sleeper }

func (s *Service) SetClockForTest(clock Clock) { s.clock = clock }

// ConfigureUserQualifications loads local user decisions. These records are
// deliberately separate from the embedded release support matrix.
func (s *Service) ConfigureUserQualifications(records []qualification.Record, saver func(qualification.Record) error) error {
	configured := make(map[string]qualification.Record, len(records))
	for _, record := range records {
		if err := qualification.Validate(record); err != nil {
			return err
		}
		key := qualification.Key(record)
		if _, exists := configured[key]; exists {
			return fmt.Errorf("duplicate user qualification for monitor %q endpoint %q", record.MonitorID, record.Connector)
		}
		configured[key] = record
	}
	s.mu.Lock()
	s.userQualifications = configured
	s.qualificationSaver = saver
	s.mu.Unlock()
	return nil
}

func (s *Service) Discover(ctx context.Context) (monitor.Snapshot, error) {
	snapshot, err := s.backend.Enumerate(ctx)
	if err != nil {
		return monitor.Snapshot{}, err
	}
	qualified := make(map[string]map[monitor.Input]bool)
	effective := make(map[string]*profiles.Profile)
	s.mu.RLock()
	userQualifications := make(map[string]qualification.Record, len(s.userQualifications))
	for id, record := range s.userQualifications {
		userQualifications[id] = record
	}
	s.mu.RUnlock()
	for i := range snapshot.Monitors {
		d := &snapshot.Monitors[i]
		if d.IdentityState == monitor.IdentityAmbiguous {
			d.SupportState = monitor.SupportAmbiguous
			continue
		}
		if d.IdentityState == monitor.IdentityInvalidEDID {
			d.SupportState = monitor.SupportUnprofiled
			continue
		}
		if d.SupportState == monitor.SupportEndpointUnavailable || d.SupportState == monitor.SupportPermissionDenied {
			continue
		}
		profile, hasProfile := s.registry.Match(d.Manufacturer, d.ProductCode, d.Model)
		if hasProfile {
			d.ProfileName = profile.Name
		}
		if record, ok := s.matrix.Match(s.platform, d.BackendName, d.ProfileName, d.EDIDSHA256, d.Connector); hasProfile && ok &&
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
			continue
		}
		if record, ok := userQualifications[qualification.DescriptorKey(*d)]; ok && qualification.Matches(record, *d) {
			localProfile := qualification.Profile(record, *d, profile)
			d.ProfileName = localProfile.Name
			d.SupportState = monitor.SupportUserQualified
			effective[d.ID] = localProfile
			allowed := make(map[monitor.Input]bool, len(record.Inputs))
			for _, value := range record.Inputs {
				allowed[value.Logical] = true
			}
			qualified[d.ID] = allowed
			continue
		}
		if hasProfile {
			d.SupportState = monitor.SupportBackendExperimental
		} else {
			d.SupportState = monitor.SupportUnprofiled
		}
	}
	s.mu.Lock()
	s.snapshot = snapshot
	s.qualifiedInputs = qualified
	s.effectiveProfiles = effective
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
	profile, ok := s.profileFor(d)
	if !ok {
		return nil, monitor.ErrUnsupportedMonitor
	}
	if !isQualified(d.SupportState) {
		return nil, monitor.ErrUnqualifiedSlice
	}
	values := profileInputs(profile)
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
	profile, ok := s.profileFor(d)
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
	if !isQualified(d.SupportState) {
		return monitor.InputValue{Raw: raw}, nil
	}
	value, known := profile.Logical(raw)
	if !known {
		return value, fmt.Errorf("%w: raw value 0x%02x", monitor.ErrUnknownInput, raw)
	}
	return value, nil
}

// DetectInputs retrieves the monitor-advertised capability string and extracts
// VCP 0x60 candidates. Detection alone never changes support state or
// authorises a write; ApproveInputs is the separate explicit persistence gate.
func (s *Service) DetectInputs(ctx context.Context, ref monitor.MonitorRef) (monitor.CapabilityReport, error) {
	descriptor, err := s.descriptor(ref)
	if err != nil {
		return monitor.CapabilityReport{}, err
	}
	lock := s.lockFor(descriptor.ID)
	lock.Lock()
	defer lock.Unlock()
	session, err := s.backend.Open(ctx, ref)
	if err != nil {
		return monitor.CapabilityReport{}, err
	}
	defer session.Close()
	reader, ok := session.(monitor.CapabilitySession)
	if !ok {
		return monitor.CapabilityReport{}, monitor.ErrCapabilitiesUnsupported
	}
	raw, err := reader.Capabilities(ctx)
	report := monitor.CapabilityReport{Raw: raw, Inputs: []monitor.InputCandidate{}, Advisory: true}
	if err != nil {
		return report, err
	}
	parsed, err := capabilities.Parse(raw)
	if err != nil {
		return report, err
	}
	report.InputSourceAdvertised = parsed.InputSourceAdvertised

	profile, hasProfile := s.profileFor(descriptor)
	s.mu.RLock()
	qualified := s.qualifiedInputs[descriptor.ID]
	s.mu.RUnlock()
	for _, rawValue := range parsed.InputValues {
		candidate := monitor.InputCandidate{Raw: rawValue, MappingSource: monitor.InputMappingUnmapped}
		if hasProfile {
			if value, known := profile.Logical(rawValue); known {
				candidate.Logical = value.Logical
				if descriptor.SupportState == monitor.SupportUserQualified {
					candidate.MappingSource = monitor.InputMappingUserQualification
				} else {
					candidate.MappingSource = monitor.InputMappingProfile
				}
			}
		}
		if candidate.Logical == "" {
			if logical, known := capabilities.StandardInput(rawValue); known {
				candidate.Logical = logical
				candidate.MappingSource = monitor.InputMappingMCCSStandard
			}
		}
		if isQualified(descriptor.SupportState) && qualified[candidate.Logical] && hasProfile {
			if value, resolveErr := profile.Resolve(candidate.Logical); resolveErr == nil && value.Raw == rawValue {
				candidate.WriteQualified = true
			}
		}
		report.Inputs = append(report.Inputs, candidate)
	}
	return report, nil
}

// ApproveInputs persists a reviewed mapping without writing VCP 0x60. It
// re-reads the monitor capability string and refuses any raw value that was not
// advertised in that read.
func (s *Service) ApproveInputs(ctx context.Context, ref monitor.MonitorRef, mappings []monitor.InputValue) error {
	descriptor, err := s.descriptor(ref)
	if err != nil {
		return err
	}
	if descriptor.SupportState == monitor.SupportSupported {
		return nil
	}
	if descriptor.IdentityState == monitor.IdentityAmbiguous || descriptor.SupportState == monitor.SupportAmbiguous {
		return monitor.ErrAmbiguousMonitor
	}
	if descriptor.IdentityState == monitor.IdentityInvalidEDID {
		return monitor.ErrUnsupportedMonitor
	}
	if descriptor.SupportState == monitor.SupportEndpointUnavailable {
		return monitor.ErrEndpointNotFound
	}
	if descriptor.SupportState == monitor.SupportPermissionDenied {
		return monitor.ErrPermissionDenied
	}
	report, err := s.DetectInputs(ctx, ref)
	if err != nil {
		return err
	}
	advertised := make(map[uint16]bool, len(report.Inputs))
	for _, candidate := range report.Inputs {
		advertised[candidate.Raw] = true
	}
	if len(mappings) == 0 {
		return fmt.Errorf("user qualification requires at least one accepted input")
	}
	seenLogical := make(map[monitor.Input]bool, len(mappings))
	seenRaw := make(map[uint16]bool, len(mappings))
	for _, value := range mappings {
		if !qualification.KnownInput(value.Logical) || !advertised[value.Raw] || seenLogical[value.Logical] || seenRaw[value.Raw] {
			return fmt.Errorf("input mapping %q=0x%02x was not uniquely reviewed and advertised", value.Logical, value.Raw)
		}
		seenLogical[value.Logical] = true
		seenRaw[value.Raw] = true
	}
	record, err := qualification.New(descriptor, mappings, report.Raw, s.clock.Now())
	if err != nil {
		return err
	}
	s.mu.RLock()
	saver := s.qualificationSaver
	s.mu.RUnlock()
	if saver == nil {
		return monitor.ErrQualificationUnavailable
	}
	if err := saver(record); err != nil {
		return fmt.Errorf("persist user qualification: %w", err)
	}
	base, _ := s.registry.Match(descriptor.Manufacturer, descriptor.ProductCode, descriptor.Model)
	localProfile := qualification.Profile(record, descriptor, base)
	allowed := make(map[monitor.Input]bool, len(record.Inputs))
	for _, value := range record.Inputs {
		allowed[value.Logical] = true
	}
	s.mu.Lock()
	if ref.Generation != s.snapshot.Generation {
		s.mu.Unlock()
		return monitor.ErrStaleEnumeration
	}
	monitorIndex := -1
	for index := range s.snapshot.Monitors {
		if s.snapshot.Monitors[index].ID == descriptor.ID {
			monitorIndex = index
			break
		}
	}
	if monitorIndex < 0 {
		s.mu.Unlock()
		return monitor.ErrMonitorNotFound
	}
	if !qualification.Matches(record, s.snapshot.Monitors[monitorIndex]) {
		s.mu.Unlock()
		return monitor.ErrStaleEnumeration
	}
	s.userQualifications[qualification.Key(record)] = record
	s.effectiveProfiles[descriptor.ID] = localProfile
	s.qualifiedInputs[descriptor.ID] = allowed
	s.snapshot.Monitors[monitorIndex].ProfileName = localProfile.Name
	s.snapshot.Monitors[monitorIndex].SupportState = monitor.SupportUserQualified
	s.mu.Unlock()
	return nil
}

func (s *Service) Switch(ctx context.Context, ref monitor.MonitorRef, input monitor.Input) (monitor.SwitchResult, error) {
	d, err := s.descriptor(ref)
	if err != nil {
		return monitor.SwitchResult{}, err
	}
	if !isQualified(d.SupportState) {
		return monitor.SwitchResult{}, monitor.ErrUnqualifiedSlice
	}
	profile, ok := s.profileFor(d)
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
	if d.SupportState == monitor.SupportUserQualified {
		if err := s.validateUserQualifiedTarget(ctx, ref, d, target); err != nil {
			return monitor.SwitchResult{}, err
		}
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
	raw, verificationAttempts, err := s.readWithRetry(ctx, session, profile)
	result.VerificationAttempts = verificationAttempts
	if err != nil {
		if errors.Is(err, monitor.ErrDisconnected) || errors.Is(err, monitor.ErrEndpointNotFound) {
			result.Verification = monitor.VerificationDisplayPathChanged
			result.VerificationIssue = verification.Issue(err)
			result.Duration = s.clock.Now().Sub(started)
			return result, nil
		}
		if verification.AssumableReadback(err) {
			result.Verification = monitor.VerificationAssumedSuccess
			result.VerificationIssue = verification.Issue(err)
			result.Duration = s.clock.Now().Sub(started)
			return result, nil
		}
		result.Duration = s.clock.Now().Sub(started)
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

func (s *Service) validateUserQualifiedTarget(ctx context.Context, ref monitor.MonitorRef, descriptor monitor.Descriptor, target monitor.InputValue) error {
	s.mu.RLock()
	record, exists := s.userQualifications[qualification.DescriptorKey(descriptor)]
	s.mu.RUnlock()
	if !exists || !qualification.Matches(record, descriptor) {
		return fmt.Errorf("%w: %w", monitor.ErrUnqualifiedSlice, monitor.ErrUserQualificationStale)
	}
	report, err := s.DetectInputs(ctx, ref)
	if err != nil {
		return fmt.Errorf("%w: revalidate user qualification: %v", monitor.ErrUnqualifiedSlice, err)
	}
	if !qualification.MatchesCapabilities(record, report.Raw) {
		return fmt.Errorf("%w: %w", monitor.ErrUnqualifiedSlice, monitor.ErrUserQualificationStale)
	}
	for _, candidate := range report.Inputs {
		if candidate.Raw == target.Raw {
			return nil
		}
	}
	return fmt.Errorf("%w: %w", monitor.ErrUnqualifiedSlice, monitor.ErrUserQualificationStale)
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

func (s *Service) profileFor(descriptor monitor.Descriptor) (*profiles.Profile, bool) {
	s.mu.RLock()
	effective := s.effectiveProfiles[descriptor.ID]
	s.mu.RUnlock()
	if effective != nil {
		return effective, true
	}
	return s.registry.ByName(descriptor.ProfileName)
}

func profileInputs(profile *profiles.Profile) []monitor.InputValue {
	values := make([]monitor.InputValue, 0, len(profile.Inputs))
	for logical, raw := range profile.Inputs {
		values = append(values, monitor.InputValue{Logical: logical, Raw: raw})
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Logical < values[j].Logical })
	return values
}

func isQualified(state monitor.SupportState) bool {
	return state == monitor.SupportSupported || state == monitor.SupportUserQualified
}

func (s *Service) lockFor(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locks[id] == nil {
		s.locks[id] = &sync.Mutex{}
	}
	return s.locks[id]
}
