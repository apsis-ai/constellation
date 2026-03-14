package mux

import (
	"context"
	"strings"
	"testing"
)

// testCallbacks returns ParserCallbacks wired to simple test doubles.
func testCallbacks() (ParserCallbacks, *callbackRecorder) {
	rec := &callbackRecorder{}
	cb := ParserCallbacks{
		ProcessTextWithStatus: func(sessionID, text string) string {
			rec.processedTexts = append(rec.processedTexts, text)
			return text // passthrough
		},
		TrackAction: func(sessionID, tool string, args map[string]interface{}) {
			rec.trackedActions = append(rec.trackedActions, trackedAction{tool: tool, args: args})
		},
		AppendConversation: func(sessionID string, entry ConversationEntry) {
			rec.conversations = append(rec.conversations, entry)
		},
		DebounceSummary: func(sessionID string) {
			rec.summaryDebounces++
		},
	}
	return cb, rec
}

type trackedAction struct {
	tool string
	args map[string]interface{}
}

type callbackRecorder struct {
	processedTexts   []string
	trackedActions   []trackedAction
	conversations    []ConversationEntry
	summaryDebounces int
}

// --- ClaudeParser standalone tests ---

func TestClaudeParser_TextAndResult(t *testing.T) {
	// Arrange
	input := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello world"}]}}`,
		`{"type":"result","session_id":"sess-123","modelUsage":{"model1":{"inputTokens":100,"outputTokens":50,"cacheCreationInputTokens":10,"cacheReadInputTokens":5,"contextWindow":200000}}}`,
	}, "\n")
	cb, _ := testCallbacks()
	p := &ClaudeParser{Callbacks: cb}
	ch := make(chan ChanEvent, 32)

	// Act
	result := p.Parse(context.Background(), "test-session", strings.NewReader(input), ch)
	close(ch)

	// Assert
	if result.ConversationID != "sess-123" {
		t.Errorf("expected conversation ID 'sess-123', got %q", result.ConversationID)
	}
	if result.TokenUsage != 165 { // 100+50+10+5
		t.Errorf("expected token usage 165, got %d", result.TokenUsage)
	}
	var texts []string
	for evt := range ch {
		if evt.Type == ChanText {
			texts = append(texts, evt.Text)
		}
	}
	combined := strings.Join(texts, "")
	if !strings.Contains(combined, "Hello world") {
		t.Errorf("expected 'Hello world' in output, got %q", combined)
	}
}

func TestClaudeParser_ToolUse(t *testing.T) {
	// Arrange
	input := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"read_file","input":{"path":"/tmp/test.txt"}}]}}`
	cb, rec := testCallbacks()
	p := &ClaudeParser{Callbacks: cb}
	ch := make(chan ChanEvent, 32)

	// Act
	p.Parse(context.Background(), "test-session", strings.NewReader(input), ch)
	close(ch)

	// Assert
	var actions []string
	for evt := range ch {
		if evt.Type == ChanAction {
			actions = append(actions, evt.JSON)
		}
	}
	if len(actions) == 0 {
		t.Fatal("expected at least one action event")
	}
	if !strings.Contains(actions[0], "read_file") {
		t.Errorf("expected 'read_file' in action JSON, got %q", actions[0])
	}
	if len(rec.trackedActions) == 0 {
		t.Fatal("expected TrackAction callback to be called")
	}
	if rec.trackedActions[0].tool != "read_file" {
		t.Errorf("expected tracked tool 'read_file', got %q", rec.trackedActions[0].tool)
	}
}

func TestClaudeParser_AskUser(t *testing.T) {
	// Arrange
	input := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"ask_user","input":{"question":"What next?","options":["a","b"]}}]}}`
	cb, _ := testCallbacks()
	var askUserCalled bool
	var capturedPending AskUserPending
	cb.HandleAskUser = func(sid string, pending AskUserPending) {
		askUserCalled = true
		capturedPending = pending
	}
	var killCalled bool
	cb.KillProcess = func(sid string) {
		killCalled = true
	}
	p := &ClaudeParser{Callbacks: cb}
	ch := make(chan ChanEvent, 32)

	// Act
	p.Parse(context.Background(), "test-session", strings.NewReader(input), ch)
	close(ch)

	// Assert
	var askEvents []string
	for evt := range ch {
		if evt.Type == ChanAskUser {
			askEvents = append(askEvents, evt.JSON)
		}
	}
	if len(askEvents) == 0 {
		t.Fatal("expected ChanAskUser event")
	}
	if !strings.Contains(askEvents[0], "What next?") {
		t.Errorf("expected question in ask event, got %q", askEvents[0])
	}
	if !askUserCalled {
		t.Error("expected HandleAskUser callback to be called")
	}
	if capturedPending.Question != "What next?" {
		t.Errorf("expected pending question 'What next?', got %q", capturedPending.Question)
	}
	if !killCalled {
		t.Error("expected KillProcess callback to be called")
	}
}

func TestClaudeParser_NonJSON(t *testing.T) {
	// Arrange
	input := "plain text line\nanother line\n"
	cb, _ := testCallbacks()
	p := &ClaudeParser{Callbacks: cb}
	ch := make(chan ChanEvent, 32)

	// Act
	result := p.Parse(context.Background(), "test-session", strings.NewReader(input), ch)
	close(ch)

	// Assert
	if !strings.Contains(result.FullText, "plain text line") {
		t.Errorf("expected non-JSON lines in FullText, got %q", result.FullText)
	}
}

// --- CodexParser standalone tests ---

func TestCodexParser_TextAndUsage(t *testing.T) {
	// Arrange
	input := strings.Join([]string{
		`{"type":"item.completed","item":{"type":"agent_message","text":"Codex response"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":200,"cached_input_tokens":50,"output_tokens":100}}`,
	}, "\n")
	cb, _ := testCallbacks()
	p := &CodexParser{Callbacks: cb}
	ch := make(chan ChanEvent, 32)

	// Act
	result := p.Parse(context.Background(), "test-session", strings.NewReader(input), ch)
	close(ch)

	// Assert
	if result.TokenUsage != 350 { // 200+50+100
		t.Errorf("expected token usage 350, got %d", result.TokenUsage)
	}
	var texts []string
	for evt := range ch {
		if evt.Type == ChanText {
			texts = append(texts, evt.Text)
		}
	}
	combined := strings.Join(texts, "")
	if !strings.Contains(combined, "Codex response") {
		t.Errorf("expected 'Codex response' in output, got %q", combined)
	}
}

func TestCodexParser_ItemDelta(t *testing.T) {
	// Arrange
	input := `{"type":"item.delta","delta":{"type":"text","text":"streaming chunk"}}`
	cb, rec := testCallbacks()
	p := &CodexParser{Callbacks: cb}
	ch := make(chan ChanEvent, 32)

	// Act
	p.Parse(context.Background(), "test-session", strings.NewReader(input), ch)
	close(ch)

	// Assert
	if len(rec.processedTexts) == 0 {
		t.Fatal("expected ProcessTextWithStatus to be called")
	}
	if rec.processedTexts[0] != "streaming chunk" {
		t.Errorf("expected 'streaming chunk', got %q", rec.processedTexts[0])
	}
}

// --- OpenCodeParser standalone tests ---

func TestOpenCodeParser_TextAndSession(t *testing.T) {
	// Arrange
	input := strings.Join([]string{
		`{"type":"text","part":{"text":"OpenCode response"},"sessionID":"oc-sess-1"}`,
		`{"type":"step_finish","part":{"tokens":{"total":500}}}`,
	}, "\n")
	cb, _ := testCallbacks()
	p := &OpenCodeParser{Callbacks: cb}
	ch := make(chan ChanEvent, 32)

	// Act
	result := p.Parse(context.Background(), "test-session", strings.NewReader(input), ch)
	close(ch)

	// Assert
	if result.ConversationID != "oc-sess-1" {
		t.Errorf("expected conversation ID 'oc-sess-1', got %q", result.ConversationID)
	}
	if result.TokenUsage != 500 {
		t.Errorf("expected token usage 500, got %d", result.TokenUsage)
	}
	var texts []string
	for evt := range ch {
		if evt.Type == ChanText {
			texts = append(texts, evt.Text)
		}
	}
	combined := strings.Join(texts, "")
	if !strings.Contains(combined, "OpenCode response") {
		t.Errorf("expected 'OpenCode response' in output, got %q", combined)
	}
}

func TestOpenCodeParser_ToolUse(t *testing.T) {
	// Arrange
	input := `{"type":"tool_use","part":{"tool":"write_file","state":{"input":{"path":"/tmp/out.txt"},"output":"done"}}}`
	cb, rec := testCallbacks()
	p := &OpenCodeParser{Callbacks: cb}
	ch := make(chan ChanEvent, 32)

	// Act
	p.Parse(context.Background(), "test-session", strings.NewReader(input), ch)
	close(ch)

	// Assert
	var actions []string
	for evt := range ch {
		if evt.Type == ChanAction {
			actions = append(actions, evt.JSON)
		}
	}
	if len(actions) == 0 {
		t.Fatal("expected at least one action event")
	}
	if !strings.Contains(actions[0], "write_file") {
		t.Errorf("expected 'write_file' in action JSON, got %q", actions[0])
	}
	if len(rec.trackedActions) == 0 || rec.trackedActions[0].tool != "write_file" {
		t.Errorf("expected tracked tool 'write_file'")
	}
	if len(rec.conversations) == 0 {
		t.Fatal("expected conversation entry")
	}
	if rec.conversations[0].Tool != "write_file" {
		t.Errorf("expected conversation tool 'write_file', got %q", rec.conversations[0].Tool)
	}
}

func TestOpenCodeParser_Error(t *testing.T) {
	// Arrange
	input := `{"type":"error","error":{"message":"rate limit exceeded"}}`
	cb, _ := testCallbacks()
	p := &OpenCodeParser{Callbacks: cb}
	ch := make(chan ChanEvent, 32)

	// Act
	p.Parse(context.Background(), "test-session", strings.NewReader(input), ch)
	close(ch)

	// Assert
	var texts []string
	for evt := range ch {
		if evt.Type == ChanText {
			texts = append(texts, evt.Text)
		}
	}
	combined := strings.Join(texts, "")
	if !strings.Contains(combined, "rate limit exceeded") {
		t.Errorf("expected error message in output, got %q", combined)
	}
}

// --- CursorParser standalone tests ---

func TestCursorParser_TextAndResult(t *testing.T) {
	// Arrange
	input := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Cursor reply"}]}}`,
		`{"type":"result","session_id":"cur-sess","usage":{"inputTokens":300,"outputTokens":150,"cacheReadTokens":20,"cacheWriteTokens":10}}`,
	}, "\n")
	cb, _ := testCallbacks()
	p := &CursorParser{Callbacks: cb}
	ch := make(chan ChanEvent, 32)

	// Act
	result := p.Parse(context.Background(), "test-session", strings.NewReader(input), ch)
	close(ch)

	// Assert
	if result.ConversationID != "cur-sess" {
		t.Errorf("expected conversation ID 'cur-sess', got %q", result.ConversationID)
	}
	if result.TokenUsage != 480 { // 300+150+20+10
		t.Errorf("expected token usage 480, got %d", result.TokenUsage)
	}
}

func TestCursorParser_Error(t *testing.T) {
	// Arrange
	input := `{"type":"error","is_error":true,"result":"auth failed"}`
	cb, _ := testCallbacks()
	p := &CursorParser{Callbacks: cb}
	ch := make(chan ChanEvent, 32)

	// Act
	p.Parse(context.Background(), "test-session", strings.NewReader(input), ch)
	close(ch)

	// Assert
	var texts []string
	for evt := range ch {
		if evt.Type == ChanText {
			texts = append(texts, evt.Text)
		}
	}
	combined := strings.Join(texts, "")
	if !strings.Contains(combined, "auth failed") {
		t.Errorf("expected error in output, got %q", combined)
	}
}

// --- RawParser standalone tests ---

func TestRawParser_StreamsAllLines(t *testing.T) {
	// Arrange
	input := "first line\nsecond line\n\nthird line\n"
	cb, rec := testCallbacks()
	p := &RawParser{Callbacks: cb}
	ch := make(chan ChanEvent, 32)

	// Act
	result := p.Parse(context.Background(), "test-session", strings.NewReader(input), ch)
	close(ch)

	// Assert - every non-empty line should become a ChanText event
	var texts []string
	for evt := range ch {
		if evt.Type == ChanText {
			texts = append(texts, evt.Text)
		}
	}
	if len(texts) != 3 {
		t.Errorf("expected 3 text events, got %d", len(texts))
	}
	if !strings.Contains(result.FullText, "first line") {
		t.Errorf("expected 'first line' in FullText, got %q", result.FullText)
	}
	if !strings.Contains(result.FullText, "third line") {
		t.Errorf("expected 'third line' in FullText, got %q", result.FullText)
	}
	// ProcessTextWithStatus should have been called for each non-empty line
	if len(rec.processedTexts) != 3 {
		t.Errorf("expected 3 calls to ProcessTextWithStatus, got %d", len(rec.processedTexts))
	}
}

func TestRawParser_EmptyInput(t *testing.T) {
	// Arrange
	input := ""
	cb, _ := testCallbacks()
	p := &RawParser{Callbacks: cb}
	ch := make(chan ChanEvent, 32)

	// Act
	result := p.Parse(context.Background(), "test-session", strings.NewReader(input), ch)
	close(ch)

	// Assert
	if result.FullText != "" {
		t.Errorf("expected empty FullText, got %q", result.FullText)
	}
	var count int
	for range ch {
		count++
	}
	if count != 0 {
		t.Errorf("expected no events, got %d", count)
	}
}
