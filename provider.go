package mux

import (
	"context"
	"io"
)

// Provider executes prompts and streams results.
type Provider interface {
	// ID returns the unique provider identifier (e.g., "claude", "codex").
	ID() string

	// Execute runs a prompt and streams events to the channel.
	Execute(ctx context.Context, req ProviderRequest, ch chan<- ChanEvent) (ProviderResult, error)

	// SupportsResume returns true if the provider can resume conversations.
	SupportsResume() bool

	// Validate checks if the provider is available/configured correctly.
	Validate() error

	// ListModels returns available models.
	ListModels(ctx context.Context) ([]ModelInfo, error)

	// DefaultModel returns the default model ID.
	DefaultModel() string
}

// ProviderRequest contains everything needed to execute a prompt.
type ProviderRequest struct {
	SessionID      string
	Prompt         string
	Model          string
	Effort         string
	SubAgent       string
	ConversationID string
	Attachments    []AttachmentRef
	MCPConfig      string
	Env            []string
}

// ProviderResult is returned after execution completes.
type ProviderResult struct {
	FullText       string
	ConversationID string
	TokenUsage     int
	UsagePct       float64
}

// ModelInfo describes an available model.
type ModelInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	ContextSize int      `json:"context_size,omitempty"`
	Efforts     []string `json:"efforts,omitempty"`
}

// OutputParser parses streaming output from a provider.
type OutputParser interface {
	Parse(ctx context.Context, sessionID string, r io.Reader, ch chan<- ChanEvent) streamResult
}

// ParserCallbacks provides Manager functionality to parsers without direct coupling.
type ParserCallbacks struct {
	ProcessTextWithStatus func(sessionID, text string) string
	TrackAction           func(sessionID, tool string, args map[string]interface{})
	AppendConversation    func(sessionID string, entry ConversationEntry)
	DebounceSummary       func(sessionID string)
	// Claude-specific: handle ask_user tool detection
	HandleAskUser func(sessionID string, pending AskUserPending)
	// Claude-specific: kill the running process (for ask_user flow)
	KillProcess func(sessionID string)
}
