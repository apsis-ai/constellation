package mux

import (
	"context"
	"io"
	"syscall"
	"time"
)

// streamClaudeOutput delegates to ClaudeParser.
func (m *Manager) streamClaudeOutput(sessionID string, r io.Reader, ch chan<- ChanEvent) streamResult {
	p := &ClaudeParser{Callbacks: m.parserCallbacks(sessionID)}
	return p.Parse(context.Background(), sessionID, r, ch)
}

// streamCodexOutput delegates to CodexParser.
func (m *Manager) streamCodexOutput(sessionID string, r io.Reader, ch chan<- ChanEvent) streamResult {
	p := &CodexParser{Callbacks: m.parserCallbacks(sessionID)}
	return p.Parse(context.Background(), sessionID, r, ch)
}

// streamOpenCodeOutput delegates to OpenCodeParser.
func (m *Manager) streamOpenCodeOutput(sessionID string, r io.Reader, ch chan<- ChanEvent) streamResult {
	p := &OpenCodeParser{Callbacks: m.parserCallbacks(sessionID)}
	return p.Parse(context.Background(), sessionID, r, ch)
}

// streamCursorOutput delegates to CursorParser.
func (m *Manager) streamCursorOutput(sessionID string, r io.Reader, ch chan<- ChanEvent) streamResult {
	p := &CursorParser{Callbacks: m.parserCallbacks(sessionID)}
	return p.Parse(context.Background(), sessionID, r, ch)
}

// parserCallbacks returns ParserCallbacks wired to this Manager.
func (m *Manager) parserCallbacks(sessionID string) ParserCallbacks {
	return ParserCallbacks{
		ProcessTextWithStatus: m.processTextWithStatus,
		TrackAction:           m.trackAction,
		AppendConversation:    m.appendConversation,
		DebounceSummary:       m.debounceSummary,
		HandleAskUser: func(sid string, pending AskUserPending) {
			m.mu.Lock()
			m.askUserPending[sid] = pending
			m.mu.Unlock()
		},
		KillProcess: func(sid string) {
			m.mu.Lock()
			proc, hasProc := m.activeProcesses[sid]
			m.mu.Unlock()
			go func() {
				time.Sleep(500 * time.Millisecond)
				if hasProc {
					_ = syscall.Kill(-proc.Pid, syscall.SIGKILL)
				}
			}()
		},
	}
}
