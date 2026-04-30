package mux

// MCPConfigProvider generates MCP configuration for agent subprocesses.
// The library calls this before spawning an agent to get the MCP JSON config.
type MCPConfigProvider interface {
	// MCPConfig returns the MCP JSON config string for the given session and agent.
	// Returns empty string if no MCP config is needed.
	MCPConfig(sessionID, agent string) (string, error)
}

// ToolExecutor handles tool calls from HTTP-based agent loops (e.g., Tela).
// For subprocess-based agents (claude, codex), tools are handled via MCP.
type ToolExecutor interface {
	// ExecuteTool executes a tool call and returns the result.
	ExecuteTool(sessionID, toolName string, args map[string]interface{}) (string, error)
}

// ActionSummaryFormatter converts tool calls into human-readable summaries.
type ActionSummaryFormatter interface {
	// FormatAction returns a human-readable summary of a tool action.
	FormatAction(tool string, args map[string]interface{}) string
}

// HandoffHandler is called when a handoff is triggered.
type HandoffHandler interface {
	// HandleHandoff persists the handoff state.
	HandleHandoff(sessionID, summary, currentState, pendingTasks string) error
}

// TitleGenerator generates session titles from prompts.
type TitleGenerator interface {
	// GenerateTitle returns a short title for a prompt.
	GenerateTitle(prompt string) string
}

// SummaryGenerator generates session summaries from conversation history.
type SummaryGenerator interface {
	// GenerateSummary returns a SessionSummary for the given conversation.
	GenerateSummary(entries []ConversationEntry) (*SessionSummary, error)
}

// Transcriber abstracts speech-to-text.
type Transcriber interface {
	Transcribe(audioPath, language string) (string, error)
}

// IOLocker abstracts I/O lock management for session lifecycle.
// Implementations serialize access to shared resources (e.g., screen input)
// so only one session controls them at a time. The Manager calls ReleaseIOLock
// automatically when a session finishes or is stopped.
type IOLocker interface {
	TryAcquireIOLock(sessionID string) bool
	ReleaseIOLock(sessionID string)
	GetIOLock() (bool, IOLock)
}

// DefaultActionFormatter provides a generic tool-name-to-sentence formatter.
type DefaultActionFormatter struct{}

func (DefaultActionFormatter) FormatAction(tool string, args map[string]interface{}) string {
	// Replace underscores with spaces, capitalize first letter
	words := ""
	for i, c := range tool {
		if c == '_' {
			words += " "
		} else if i == 0 && c >= 'a' && c <= 'z' {
			words += string(c - 32)
		} else {
			words += string(c)
		}
	}
	return words
}
