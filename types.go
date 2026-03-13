package mux

import (
	"encoding/json"
	"fmt"
	"time"
)

// SessionStatus represents the lifecycle state of a session.
type SessionStatus string

const (
	StatusActive  SessionStatus = "active"
	StatusIdle    SessionStatus = "idle"
	StatusWaiting SessionStatus = "waiting"
)

// Session represents a conversation session with an AI agent.
type Session struct {
	ID           string        `json:"id"`
	Status       SessionStatus `json:"status"`
	HandoffPath  string        `json:"handoff_path,omitempty"`
	Title        string        `json:"title"`
	LastAgent    string        `json:"last_agent"`
	LastAgentSub string        `json:"last_agent_sub"`
	LastModel    string        `json:"last_model"`
	LastEffort   string        `json:"last_effort"`
	CreatedAt    int64         `json:"created_at"`
	LastActiveAt int64         `json:"last_active_at"`
}

// Message is a single conversation turn stored in the DB.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ConversationEntry is a single line in conversation.jsonl.
type ConversationEntry struct {
	Timestamp   string                 `json:"ts"`
	Role        string                 `json:"role"`
	Content     string                 `json:"content"`
	Agent       string                 `json:"agent,omitempty"`
	Tool        string                 `json:"tool,omitempty"`
	Input       map[string]interface{} `json:"input,omitempty"`
	Result      string                 `json:"result,omitempty"`
	Summary     string                 `json:"summary,omitempty"`
	Attachments []AttachmentRef        `json:"attachments,omitempty"`
	Origin      string                 `json:"origin,omitempty"`
	DisplayAs   string                 `json:"display_as,omitempty"`
}

// AttachmentRef describes an uploaded file attached to a prompt.
type AttachmentRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
	Path string `json:"-"`
}

// QueueItem represents a follow-up message waiting to be processed.
type QueueItem struct {
	ID          string   `json:"id"`
	SessionID   string   `json:"session_id"`
	Text        string   `json:"text"`
	Position    int      `json:"position"`
	Agent       string   `json:"agent"`
	AgentSub    string   `json:"agent_sub"`
	Model       string   `json:"model"`
	Effort      string   `json:"effort"`
	Attachments []string `json:"attachments,omitempty"`
	CreatedAt   int64    `json:"created_at"`
	Source      string   `json:"source"`
	Status      string   `json:"status"`
	Transcript  string   `json:"transcript,omitempty"`
	MessageID   int64    `json:"message_id,omitempty"`
	StartedAt   int64    `json:"started_at,omitempty"`
	CompletedAt int64    `json:"completed_at,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// AskUserPending holds a pending ask_user question.
type AskUserPending struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
}

// SessionSummary is the content of summary.json.
type SessionSummary struct {
	Status    string                 `json:"status"`
	Summary   string                 `json:"summary"`
	Next      string                 `json:"next"`
	Facts     map[string]interface{} `json:"facts"`
	UpdatedAt string                 `json:"updated_at"`
}

// ActionStatus represents the current action state of a session.
type ActionStatus struct {
	Status       string `json:"status"`
	Summary      string `json:"summary"`
	Tool         string `json:"tool"`
	SessionTitle string `json:"session_title"`
	UpdatedAt    int64  `json:"updated_at"`
	QueueLength  int    `json:"queue_length"`
}

// IOLock represents the I/O mutex state.
type IOLock struct {
	SessionID string `json:"session_id"`
	LockedAt  int64  `json:"locked_at"`
}

// ChanEventType identifies event types on the Manager->consumer channel.
type ChanEventType int

const (
	ChanText    ChanEventType = iota
	ChanAction
	ChanAskUser
)

// ChanEvent is a typed event on the Manager->consumer channel.
type ChanEvent struct {
	Type ChanEventType
	Text string
	JSON string
}

// streamResult holds parsed result from agent output stream.
type streamResult struct {
	FullText       string
	ConversationID string
	UsagePct       float64
	TokenUsage     int
}

// HandoffRequest is the body for handoff operations.
type HandoffRequest struct {
	SessionID    string `json:"session_id"`
	Summary      string `json:"summary"`
	CurrentState string `json:"current_state"`
	PendingTasks string `json:"pending_tasks"`
}

// SendRequest contains all parameters for sending a prompt.
type SendRequest struct {
	Prompt        string
	SessionID     string
	Agent         string
	AgentSub      string
	Model         string
	Effort        string
	AttachmentIDs []string
}

// SendResult is returned from Manager.Send.
type SendResult struct {
	Events    <-chan ChanEvent
	SessionID string
	MessageID int64
}

// toInt converts numeric interface values to int.
func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}

// fallbackTitle generates a title from the first 5 words of a prompt.
func fallbackTitle(prompt string) string {
	words := splitWords(prompt)
	if len(words) > 5 {
		words = words[:5]
	}
	title := joinWords(words)
	if len(title) > 50 {
		title = title[:50]
	}
	return title
}

func splitWords(s string) []string {
	var words []string
	word := ""
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' {
			if word != "" {
				words = append(words, word)
				word = ""
			}
		} else {
			word += string(r)
		}
	}
	if word != "" {
		words = append(words, word)
	}
	return words
}

func joinWords(words []string) string {
	result := ""
	for i, w := range words {
		if i > 0 {
			result += " "
		}
		result += w
	}
	return result
}

// coerceStringSlice safely converts []interface{} to []string, skipping non-strings.
func coerceStringSlice(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	var out []string
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// nowUnix returns the current Unix timestamp.
func nowUnix() int64 { return time.Now().Unix() }

// nowRFC3339 returns the current UTC time in RFC3339 format.
func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

// formatDuration formats a duration for display.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}
