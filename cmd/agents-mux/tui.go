package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	mux "github.com/prxg22/agents-mux"
)

// Styles
var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#F59E0B")).
			PaddingBottom(1)

	agentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#22c55e")).
			Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ef4444")).
			Bold(true)
)

// eventMsg wraps a ChanEvent for bubbletea.
type eventMsg struct {
	event mux.ChanEvent
	done  bool
}

// tuiModel is the bubbletea model for interactive prompt streaming.
type tuiModel struct {
	sessionID string
	agent     string
	events    <-chan mux.ChanEvent
	output    strings.Builder
	spinner   spinner.Model
	done      bool
	err       error
	width     int
	height    int
}

func newTuiModel(sessionID, agent string, events <-chan mux.ChanEvent) tuiModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	return tuiModel{
		sessionID: sessionID,
		agent:     agent,
		events:    events,
		spinner:   s,
		width:     80,
		height:    24,
	}
}

func waitForEvent(events <-chan mux.ChanEvent) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-events
		if !ok {
			return eventMsg{done: true}
		}
		return eventMsg{event: evt}
	}
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, waitForEvent(m.events))
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}

	case spinner.TickMsg:
		if !m.done {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case eventMsg:
		if msg.done {
			m.done = true
			return m, tea.Quit
		}
		switch msg.event.Type {
		case mux.ChanText:
			m.output.WriteString(msg.event.Text)
		case mux.ChanAction:
			m.output.WriteString(fmt.Sprintf("\n[action] %s\n", msg.event.JSON))
		case mux.ChanAskUser:
			m.output.WriteString(fmt.Sprintf("\n[ask_user] %s\n", msg.event.JSON))
		}
		return m, waitForEvent(m.events)
	}
	return m, nil
}

func (m tuiModel) View() string {
	var sb strings.Builder

	// Header
	header := headerStyle.Render(fmt.Sprintf("Session: %s", m.sessionID))
	if m.agent != "" {
		header += " " + agentStyle.Render(fmt.Sprintf("[%s]", m.agent))
	}
	sb.WriteString(header)
	sb.WriteString("\n")

	// Body
	output := m.output.String()
	if output != "" {
		sb.WriteString(output)
	}

	// Footer
	sb.WriteString("\n")
	if m.err != nil {
		sb.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
	} else if m.done {
		sb.WriteString(dimStyle.Render("Done."))
	} else {
		sb.WriteString(m.spinner.View())
		sb.WriteString(dimStyle.Render(" Streaming..."))
	}
	sb.WriteString("\n")

	return sb.String()
}

// runPromptInteractive runs the prompt command with the TUI.
func runPromptInteractive(text string) error {
	mgr, err := newManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	result, err := mgr.Send(mux.SendRequest{
		Prompt:    text,
		SessionID: promptSessionID,
		Agent:     promptAgent,
		Model:     promptModel,
	})
	if err != nil {
		return err
	}

	agent := promptAgent
	if agent == "" {
		agent = "claude"
	}

	m := newTuiModel(result.SessionID, agent, result.Events)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}
