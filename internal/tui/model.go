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

type Model struct {
	service          Service
	preferredMonitor string
	operationID      uint64
	snapshot         monitor.Snapshot
	monitorCursor    int
	inputs           []monitor.InputValue
	inputCursor      int
	current          *monitor.InputValue
	busy             bool
	confirmInput     monitor.Input
	status           string
	lastError        error
	showHelp         bool
	width            int
	height           int
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
			return m, nil
		}
		m.current = &msg.Input
		m.lastError = nil
		m.status = "Current input read"
		return m, nil
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
		m.status = fmt.Sprintf("Switch %s", msg.Result.Verification)
		m.current = msg.Result.Observed
		return m, nil
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
		body.WriteString("\n\nup/down or j/k monitor • left/right input • g read • enter confirm/switch • r refresh • q quit")
	} else {
		body.WriteString("\n\n? help • q quit")
	}
	view := tea.NewView(body.String())
	view.AltScreen = true
	view.WindowTitle = "XDispDDCSwtchr"
	return view
}

func (m Model) handleKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	case "r":
		if m.busy {
			return m, nil
		}
		m.operationID++
		m.busy = true
		m.current = nil
		m.inputs = nil
		m.confirmInput = ""
		m.status = "Discovering external monitors..."
		return m, m.discoverCmd(m.operationID)
	case "up", "k":
		if m.busy || m.monitorCursor <= 0 {
			return m, nil
		}
		m.monitorCursor--
		m.operationID++
		return m.selectMonitor()
	case "down", "j":
		if m.busy || m.monitorCursor >= len(m.snapshot.Monitors)-1 {
			return m, nil
		}
		m.monitorCursor++
		m.operationID++
		return m.selectMonitor()
	case "left", "h":
		if !m.busy && m.inputCursor > 0 {
			m.inputCursor--
			m.confirmInput = ""
		}
		return m, nil
	case "right", "l":
		if !m.busy && m.inputCursor < len(m.inputs)-1 {
			m.inputCursor++
			m.confirmInput = ""
		}
		return m, nil
	case "g":
		if m.busy || len(m.snapshot.Monitors) == 0 {
			return m, nil
		}
		m.operationID++
		m.busy = true
		m.status = "Reading VCP 0x60..."
		return m, m.readCmd(m.operationID, m.currentRef())
	case "enter":
		if m.busy || len(m.inputs) == 0 || len(m.snapshot.Monitors) == 0 {
			return m, nil
		}
		descriptor := m.snapshot.Monitors[m.monitorCursor]
		if descriptor.SupportState != monitor.SupportSupported {
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
	m.confirmInput = ""
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

var _ tea.Model = Model{}
