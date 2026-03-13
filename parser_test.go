package mux

import (
	"strings"
	"testing"
)

func TestStreamClaudeOutput_TextAndResult(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Hello world"}]}}`,
		`{"type":"result","session_id":"sess-123","modelUsage":{"model1":{"inputTokens":100,"outputTokens":50,"cacheCreationInputTokens":10,"cacheReadInputTokens":5,"contextWindow":200000}}}`,
	}, "\n")

	cfg := tempConfig(t)
	m, _ := NewManager(cfg)
	defer m.Close()

	ch := make(chan ChanEvent, 32)
	result := m.streamClaudeOutput("test-session", strings.NewReader(input), ch)
	close(ch)

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

func TestStreamClaudeOutput_ToolUse(t *testing.T) {
	input := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"read_file","input":{"path":"/tmp/test.txt"}}]}}`

	cfg := tempConfig(t)
	m, _ := NewManager(cfg)
	defer m.Close()

	ch := make(chan ChanEvent, 32)
	m.streamClaudeOutput("test-session", strings.NewReader(input), ch)
	close(ch)

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
}

func TestStreamCodexOutput_TextAndUsage(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"item.completed","item":{"type":"agent_message","text":"Codex response"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":200,"cached_input_tokens":50,"output_tokens":100}}`,
	}, "\n")

	cfg := tempConfig(t)
	m, _ := NewManager(cfg)
	defer m.Close()

	ch := make(chan ChanEvent, 32)
	result := m.streamCodexOutput("test-session", strings.NewReader(input), ch)
	close(ch)

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

func TestStreamOpenCodeOutput_TextAndSession(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"text","part":{"text":"OpenCode response"},"sessionID":"oc-sess-1"}`,
		`{"type":"step_finish","part":{"tokens":{"total":500}}}`,
	}, "\n")

	cfg := tempConfig(t)
	m, _ := NewManager(cfg)
	defer m.Close()

	ch := make(chan ChanEvent, 32)
	result := m.streamOpenCodeOutput("test-session", strings.NewReader(input), ch)
	close(ch)

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

func TestStreamOpenCodeOutput_Error(t *testing.T) {
	input := `{"type":"error","error":{"message":"rate limit exceeded"}}`

	cfg := tempConfig(t)
	m, _ := NewManager(cfg)
	defer m.Close()

	ch := make(chan ChanEvent, 32)
	m.streamOpenCodeOutput("test-session", strings.NewReader(input), ch)
	close(ch)

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

func TestStreamCursorOutput_TextAndResult(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Cursor reply"}]}}`,
		`{"type":"result","session_id":"cur-sess","usage":{"inputTokens":300,"outputTokens":150,"cacheReadTokens":20,"cacheWriteTokens":10}}`,
	}, "\n")

	cfg := tempConfig(t)
	m, _ := NewManager(cfg)
	defer m.Close()

	ch := make(chan ChanEvent, 32)
	result := m.streamCursorOutput("test-session", strings.NewReader(input), ch)
	close(ch)

	if result.ConversationID != "cur-sess" {
		t.Errorf("expected conversation ID 'cur-sess', got %q", result.ConversationID)
	}
	if result.TokenUsage != 480 { // 300+150+20+10
		t.Errorf("expected token usage 480, got %d", result.TokenUsage)
	}
}

func TestStreamCursorOutput_Error(t *testing.T) {
	input := `{"type":"error","is_error":true,"result":"auth failed"}`

	cfg := tempConfig(t)
	m, _ := NewManager(cfg)
	defer m.Close()

	ch := make(chan ChanEvent, 32)
	m.streamCursorOutput("test-session", strings.NewReader(input), ch)
	close(ch)

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

func TestStreamClaudeOutput_NonJSON(t *testing.T) {
	input := "plain text line\nanother line\n"

	cfg := tempConfig(t)
	m, _ := NewManager(cfg)
	defer m.Close()

	ch := make(chan ChanEvent, 32)
	result := m.streamClaudeOutput("test-session", strings.NewReader(input), ch)
	close(ch)

	if !strings.Contains(result.FullText, "plain text line") {
		t.Errorf("expected non-JSON lines in FullText, got %q", result.FullText)
	}
}
