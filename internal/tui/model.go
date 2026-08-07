package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/markz0r/XDispDDCSwtchr/internal/monitor"
)

type Service interface {
	Discover(context.Context) (monitor.Snapshot, error)
	Inputs(monitor.MonitorRef) ([]monitor.InputValue, error)
	GetInput(context.Context, monitor.MonitorRef) (monitor.InputValue, error)
	DetectInputs(context.Context, monitor.MonitorRef) (monitor.CapabilityReport, error)
	ApproveInputs(context.Context, monitor.MonitorRef, []monitor.InputValue) error
	Switch(context.Context, monitor.MonitorRef, monitor.Input) (monitor.SwitchResult, error)
}

type discoveryCompletedMsg struct {
	OperationID uint64
	Snapshot    monitor.Snapshot
	Err         error
}

type inputReadCompletedMsg struct {
	OperationID uint64
	Monitor     monitor.MonitorRef
	Input       monitor.InputValue
	Err         error
}

type inputWriteCompletedMsg struct {
	OperationID uint64
	Monitor     monitor.MonitorRef
	Result      monitor.SwitchResult
	Err         error
}

type capabilityReadCompletedMsg struct {
	OperationID uint64
	Monitor     monitor.MonitorRef
	Report      monitor.CapabilityReport
	Err         error
}

type qualificationCompletedMsg struct {
	OperationID uint64
	Monitor     monitor.MonitorRef
	Err         error
}

var qualificationChoices = []monitor.Input{
	"",
	monitor.InputDisplayPort1,
	monitor.InputDisplayPort2,
	monitor.InputHDMI1,
	monitor.InputHDMI2,
	monitor.InputUSBC1,
	monitor.InputUSBC2,
	monitor.InputThunderbolt1,
}

type Model struct {
	service               Service
	preferredMonitor      string
	operationID           uint64
	snapshot              monitor.Snapshot
	monitorCursor         int
	inputs                []monitor.InputValue
	inputCursor           int
	current               *monitor.InputValue
	detected              []monitor.InputCandidate
	qualificationReview   bool
	qualificationConfirm  bool
	qualificationIndex    int
	qualificationChoice   int
	qualificationMappings []monitor.InputValue
	busy                  bool
	confirmInput          monitor.Input
	status                string
	lastError             error
	showHelp              bool
	width                 int
	height                int
}

func New(service Service, preferredMonitor string) Model {
	return Model{
		service: service, preferredMonitor: preferredMonitor, operationID: 1,
		busy: true, status: "Discovering external monitors...",
	}
}

func (m Model) Init() tea.Cmd {
	return m.discoverCmd(m.operationID)
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case discoveryCompletedMsg:
		if msg.OperationID != m.operationID {
			return m, nil
		}
		m.busy = false
		if msg.Err != nil {
			m.lastError = msg.Err
			m.status = "Discovery failed"
			return m, nil
		}
		m.snapshot = msg.Snapshot
		m.monitorCursor = m.preferredIndex()
		m.lastError = nil
		m.status = fmt.Sprintf("Discovered %d monitor(s)", len(msg.Snapshot.Monitors))
		return m.selectMonitor()
	case inputReadCompletedMsg:
		if msg.OperationID != m.operationID || msg.Monitor != m.currentRef() {
			return m, nil
		}
		m.busy = false
		if msg.Err != nil {
			m.lastError = msg.Err
			m.status = "Input read failed"
			return m.continueQualificationPrompt()
		}
		m.current = &msg.Input
		m.lastError = nil
		m.status = "Current input read"
		return m.continueQualificationPrompt()
	case inputWriteCompletedMsg:
		if msg.OperationID != m.operationID || msg.Monitor != m.currentRef() {
			return m, nil
		}
		m.busy = false
		m.confirmInput = ""
		if msg.Err != nil {
			m.lastError = msg.Err
			m.status = "Input switch failed"
			return m, nil
		}
		m.lastError = nil
		switch msg.Result.Verification {
		case monitor.VerificationAssumedSuccess:
			m.status = "Assumed success [no read-back provided by display]\nPress r to rediscover and read the current input."
			m.current = nil
		case monitor.VerificationDisplayPathChanged:
			m.status = "Switch display-path-changed"
			m.current = nil
		default:
			m.status = fmt.Sprintf("Switch %s", msg.Result.Verification)
			m.current = msg.Result.Observed
		}
		return m, nil
	case capabilityReadCompletedMsg:
		if msg.OperationID != m.operationID || msg.Monitor != m.currentRef() {
			return m, nil
		}
		m.busy = false
		if msg.Err != nil {
			m.lastError = msg.Err
			m.status = "Capability detection failed"
			return m, nil
		}
		m.detected = append([]monitor.InputCandidate{}, msg.Report.Inputs...)
		m.lastError = nil
		if m.needsQualification() && len(m.detected) > 0 {
			m = m.beginQualification()
		} else if len(m.detected) == 0 {
			m.status = "No VCP 0x60 input values were advertised; qualification cannot continue"
		} else {
			m.status = fmt.Sprintf("Detected %d monitor-advertised input value(s); advisory only", len(m.detected))
		}
		return m, nil
	case qualificationCompletedMsg:
		if msg.OperationID != m.operationID || msg.Monitor != m.currentRef() {
			return m, nil
		}
		m.busy = false
		if msg.Err != nil {
			m.lastError = msg.Err
			m.qualificationConfirm = true
			m.status = "Qualification could not be saved; press enter to retry or esc to cancel"
			return m, nil
		}
		m.lastError = nil
		m.preferredMonitor = msg.Monitor.ID
		m.resetQualification()
		m.busy = true
		m.status = "User qualification saved; rediscovering monitor"
		return m, m.discoverCmd(m.operationID)
	case tea.KeyPressMsg:
		return m.handleKey(msg.String())
	}
	return m, nil
}

func (m Model) View() tea.View {
	var body strings.Builder
	body.WriteString("XDispDDCSwtchr — monitor input source\n\n")
	if len(m.snapshot.Monitors) == 0 {
		body.WriteString(m.status)
		body.WriteString("\n")
	} else {
		body.WriteString("Monitors\n")
		for i, descriptor := range m.snapshot.Monitors {
			cursor := " "
			if i == m.monitorCursor {
				cursor = ">"
			}
			body.WriteString(fmt.Sprintf("%s %s  %s  %s  [%s]\n", cursor, descriptor.Model, descriptor.Connector, descriptor.ID, descriptor.SupportState))
		}
		body.WriteString("\nInputs\n")
		for i, input := range m.inputs {
			cursor := " "
			if i == m.inputCursor {
				cursor = ">"
			}
			current := ""
			if m.current != nil && m.current.Raw == input.Raw {
				current = " (current)"
			}
			body.WriteString(fmt.Sprintf("%s %s%s\n", cursor, input.Logical, current))
		}
		if m.detected != nil {
			body.WriteString("\nAdvertised inputs (advisory)\n")
			for _, candidate := range m.detected {
				logical := string(candidate.Logical)
				if logical == "" {
					logical = "unknown"
				}
				body.WriteString(fmt.Sprintf("  0x%02x  %s  [%s]\n", candidate.Raw, logical, candidate.MappingSource))
			}
		}
		if m.qualificationReview && m.qualificationIndex < len(m.detected) {
			candidate := m.detected[m.qualificationIndex]
			suggestion := string(candidate.Logical)
			if suggestion == "" {
				suggestion = "none"
			}
			body.WriteString("\nResolve unqualified monitor\n")
			body.WriteString(fmt.Sprintf("Candidate %d of %d: raw 0x%02x\n", m.qualificationIndex+1, len(m.detected), candidate.Raw))
			body.WriteString(fmt.Sprintf("Detected suggestion: %s [%s]\n", suggestion, candidate.MappingSource))
			body.WriteString(fmt.Sprintf("Your mapping: %s\n", qualificationChoiceLabel(m.qualificationChoice)))
			body.WriteString("left/right changes mapping • enter accepts this choice • esc cancels\n")
			body.WriteString("Skipping leaves this raw value unavailable. This review does not write to the monitor.\n")
		}
		if m.qualificationConfirm {
			body.WriteString("\nConfirm local user qualification\n")
			for _, value := range m.qualificationMappings {
				body.WriteString(fmt.Sprintf("  %s = raw 0x%02x\n", value.Logical, value.Raw))
			}
			body.WriteString("This exact monitor/EDID/connector mapping will be saved locally.\n")
			body.WriteString("It is user-qualified, not vendor/release-qualified. No monitor write occurs now.\n")
			body.WriteString("enter saves • esc cancels\n")
		}
	}
	if m.busy {
		body.WriteString("\nWorking...")
	}
	if m.status != "" {
		body.WriteString("\n" + m.status)
	}
	if m.lastError != nil {
		body.WriteString("\nError: " + m.lastError.Error())
	}
	if m.showHelp {
		body.WriteString("\n\nup/down or j/k monitor • left/right input • g read • c detect capabilities • a review advisory • enter confirm/switch • r refresh • q quit")
	} else {
		body.WriteString("\n\n? help • q quit")
	}
	view := tea.NewView(body.String())
	view.AltScreen = true
	view.WindowTitle = "XDispDDCSwtchr"
	return view
}

func (m Model) handleKey(key string) (tea.Model, tea.Cmd) {
	if key == "q" || key == "ctrl+c" {
		return m, tea.Quit
	}
	if key == "?" {
		m.showHelp = !m.showHelp
		return m, nil
	}
	if key == "esc" && (m.qualificationReview || m.qualificationConfirm) {
		m.resetQualification()
		m.lastError = nil
		m.status = "User qualification cancelled; monitor remains unqualified"
		return m, nil
	}
	if m.busy {
		return m, nil
	}
	if m.qualificationReview {
		switch key {
		case "left", "h":
			m.cycleQualificationChoice(-1)
		case "right", "l":
			m.cycleQualificationChoice(1)
		case "enter":
			m.acceptQualificationChoice()
		}
		return m, nil
	}
	if m.qualificationConfirm {
		if key != "enter" {
			return m, nil
		}
		m.operationID++
		m.busy = true
		m.qualificationConfirm = false
		m.lastError = nil
		m.status = "Validating and saving user qualification..."
		return m, m.qualificationCmd(m.operationID, m.currentRef(), m.qualificationMappings)
	}

	switch key {
	case "r":
		m.operationID++
		m.busy = true
		m.current = nil
		m.inputs = nil
		m.detected = nil
		m.confirmInput = ""
		m.resetQualification()
		m.status = "Discovering external monitors..."
		return m, m.discoverCmd(m.operationID)
	case "up", "k":
		if m.monitorCursor <= 0 {
			return m, nil
		}
		m.monitorCursor--
		m.operationID++
		return m.selectMonitor()
	case "down", "j":
		if m.monitorCursor >= len(m.snapshot.Monitors)-1 {
			return m, nil
		}
		m.monitorCursor++
		m.operationID++
		return m.selectMonitor()
	case "left", "h":
		if m.inputCursor > 0 {
			m.inputCursor--
			m.confirmInput = ""
		}
		return m, nil
	case "right", "l":
		if m.inputCursor < len(m.inputs)-1 {
			m.inputCursor++
			m.confirmInput = ""
		}
		return m, nil
	case "g":
		if len(m.snapshot.Monitors) == 0 {
			return m, nil
		}
		m.operationID++
		m.busy = true
		m.status = "Reading VCP 0x60..."
		return m, m.readCmd(m.operationID, m.currentRef())
	case "c":
		if len(m.snapshot.Monitors) == 0 {
			return m, nil
		}
		m.operationID++
		m.busy = true
		m.status = "Reading DDC/CI capabilities..."
		return m, m.capabilitiesCmd(m.operationID, m.currentRef())
	case "a":
		if len(m.snapshot.Monitors) == 0 || !canUserQualify(m.snapshot.Monitors[m.monitorCursor]) {
			return m, nil
		}
		if len(m.detected) == 0 {
			m.operationID++
			m.busy = true
			m.status = "Reading DDC/CI capabilities before qualification..."
			return m, m.capabilitiesCmd(m.operationID, m.currentRef())
		}
		m = m.beginQualification()
		return m, nil
	case "enter":
		if len(m.inputs) == 0 || len(m.snapshot.Monitors) == 0 {
			return m, nil
		}
		descriptor := m.snapshot.Monitors[m.monitorCursor]
		if descriptor.SupportState != monitor.SupportSupported && descriptor.SupportState != monitor.SupportUserQualified {
			m.lastError = monitor.ErrUnqualifiedSlice
			m.status = "Switch blocked: this exact slice is not qualified"
			return m, nil
		}
		target := m.inputs[m.inputCursor]
		if m.confirmInput != target.Logical {
			m.confirmInput = target.Logical
			m.status = fmt.Sprintf("Press enter again to switch to %s", target.Logical)
			return m, nil
		}
		m.operationID++
		m.busy = true
		m.status = fmt.Sprintf("Switching to %s...", target.Logical)
		return m, m.writeCmd(m.operationID, m.currentRef(), target.Logical)
	}
	return m, nil
}

func (m Model) selectMonitor() (tea.Model, tea.Cmd) {
	m.inputs = nil
	m.inputCursor = 0
	m.current = nil
	m.detected = nil
	m.confirmInput = ""
	m.resetQualification()
	if len(m.snapshot.Monitors) == 0 {
		return m, nil
	}
	ref := m.currentRef()
	inputs, err := m.service.Inputs(ref)
	if err != nil {
		m.inputs = nil
		m.lastError = nil
		m.status = "No qualified inputs; reading current raw value"
	} else {
		m.inputs = inputs
	}
	m.busy = true
	m.status = "Reading VCP 0x60..."
	return m, m.readCmd(m.operationID, ref)
}

func (m Model) continueQualificationPrompt() (tea.Model, tea.Cmd) {
	if !m.needsQualification() || m.detected != nil {
		return m, nil
	}
	m.operationID++
	m.busy = true
	m.lastError = nil
	m.status = "Monitor is not qualified; reading advertised inputs for guided resolution..."
	return m, m.capabilitiesCmd(m.operationID, m.currentRef())
}

func (m Model) needsQualification() bool {
	if len(m.snapshot.Monitors) == 0 || m.monitorCursor < 0 || m.monitorCursor >= len(m.snapshot.Monitors) {
		return false
	}
	return canUserQualify(m.snapshot.Monitors[m.monitorCursor])
}

func canUserQualify(descriptor monitor.Descriptor) bool {
	if descriptor.IdentityState == monitor.IdentityAmbiguous || descriptor.IdentityState == monitor.IdentityInvalidEDID {
		return false
	}
	switch descriptor.SupportState {
	case monitor.SupportSupported, monitor.SupportUserQualified, monitor.SupportAmbiguous,
		monitor.SupportEndpointUnavailable, monitor.SupportPermissionDenied:
		return false
	default:
		return true
	}
}

func (m Model) beginQualification() Model {
	m.qualificationReview = true
	m.qualificationConfirm = false
	m.qualificationIndex = 0
	m.qualificationMappings = nil
	m.qualificationChoice = m.suggestedQualificationChoice(m.detected[0])
	m.confirmInput = ""
	m.lastError = nil
	m.status = "Qualification required: resolve advisory by reviewing every advertised raw value"
	return m
}

func (m *Model) resetQualification() {
	m.qualificationReview = false
	m.qualificationConfirm = false
	m.qualificationIndex = 0
	m.qualificationChoice = 0
	m.qualificationMappings = nil
}

func (m *Model) cycleQualificationChoice(delta int) {
	count := len(qualificationChoices)
	m.qualificationChoice = (m.qualificationChoice + delta + count) % count
	m.lastError = nil
}

func (m *Model) acceptQualificationChoice() {
	if m.qualificationIndex < 0 || m.qualificationIndex >= len(m.detected) {
		return
	}
	candidate := m.detected[m.qualificationIndex]
	logical := qualificationChoices[m.qualificationChoice]
	if logical != "" {
		for _, accepted := range m.qualificationMappings {
			if accepted.Logical == logical {
				m.lastError = fmt.Errorf("%s is already assigned; choose another label or skip", logical)
				return
			}
			if accepted.Raw == candidate.Raw {
				m.lastError = fmt.Errorf("raw 0x%02x is already assigned", candidate.Raw)
				return
			}
		}
		m.qualificationMappings = append(m.qualificationMappings, monitor.InputValue{Logical: logical, Raw: candidate.Raw})
	}
	m.lastError = nil
	m.qualificationIndex++
	if m.qualificationIndex >= len(m.detected) {
		m.qualificationReview = false
		if len(m.qualificationMappings) == 0 {
			m.lastError = fmt.Errorf("at least one advertised input must be accepted")
			m.status = "No mappings accepted; press a to restart or leave the monitor unqualified"
			return
		}
		m.qualificationConfirm = true
		m.status = "Review complete; confirm persistence of the local user qualification"
		return
	}
	m.qualificationChoice = m.suggestedQualificationChoice(m.detected[m.qualificationIndex])
	m.status = fmt.Sprintf("Review advertised value %d of %d", m.qualificationIndex+1, len(m.detected))
}

func (m Model) suggestedQualificationChoice(candidate monitor.InputCandidate) int {
	for _, accepted := range m.qualificationMappings {
		if accepted.Logical == candidate.Logical {
			return 0
		}
	}
	for index, logical := range qualificationChoices {
		if logical != "" && logical == candidate.Logical {
			return index
		}
	}
	return 0
}

func qualificationChoiceLabel(index int) string {
	if index <= 0 || index >= len(qualificationChoices) {
		return "skip / unavailable"
	}
	return string(qualificationChoices[index])
}

func (m Model) preferredIndex() int {
	for i, descriptor := range m.snapshot.Monitors {
		if descriptor.ID == m.preferredMonitor {
			return i
		}
	}
	return 0
}

func (m Model) currentRef() monitor.MonitorRef {
	if len(m.snapshot.Monitors) == 0 || m.monitorCursor < 0 || m.monitorCursor >= len(m.snapshot.Monitors) {
		return monitor.MonitorRef{}
	}
	return monitor.MonitorRef{ID: m.snapshot.Monitors[m.monitorCursor].ID, Generation: m.snapshot.Generation}
}

func (m Model) discoverCmd(operationID uint64) tea.Cmd {
	return func() tea.Msg {
		snapshot, err := m.service.Discover(context.Background())
		return discoveryCompletedMsg{OperationID: operationID, Snapshot: snapshot, Err: err}
	}
}

func (m Model) readCmd(operationID uint64, ref monitor.MonitorRef) tea.Cmd {
	return func() tea.Msg {
		input, err := m.service.GetInput(context.Background(), ref)
		return inputReadCompletedMsg{OperationID: operationID, Monitor: ref, Input: input, Err: err}
	}
}

func (m Model) writeCmd(operationID uint64, ref monitor.MonitorRef, target monitor.Input) tea.Cmd {
	return func() tea.Msg {
		result, err := m.service.Switch(context.Background(), ref, target)
		return inputWriteCompletedMsg{OperationID: operationID, Monitor: ref, Result: result, Err: err}
	}
}

func (m Model) capabilitiesCmd(operationID uint64, ref monitor.MonitorRef) tea.Cmd {
	return func() tea.Msg {
		report, err := m.service.DetectInputs(context.Background(), ref)
		return capabilityReadCompletedMsg{OperationID: operationID, Monitor: ref, Report: report, Err: err}
	}
}

func (m Model) qualificationCmd(operationID uint64, ref monitor.MonitorRef, mappings []monitor.InputValue) tea.Cmd {
	accepted := append([]monitor.InputValue(nil), mappings...)
	return func() tea.Msg {
		err := m.service.ApproveInputs(context.Background(), ref, accepted)
		return qualificationCompletedMsg{OperationID: operationID, Monitor: ref, Err: err}
	}
}

var _ tea.Model = Model{}
