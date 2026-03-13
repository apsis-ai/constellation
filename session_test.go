package mux

import (
	"os"
	"path/filepath"
	"testing"
)

func tempConfig(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	return Config{
		DBPath:     filepath.Join(dir, "test.db"),
		SessionDir: filepath.Join(dir, "sessions"),
		HandoffDir: filepath.Join(dir, "handoffs"),
	}
}

func TestNewManager_RequiresDBPath(t *testing.T) {
	_, err := NewManager(Config{})
	if err == nil {
		t.Fatal("expected error when DBPath is empty")
	}
}

func TestNewManager_DefaultConfig(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	if m.config.IdleTimeout == 0 {
		t.Error("expected default IdleTimeout to be set")
	}
	if m.config.ActionFmt == nil {
		t.Error("expected default ActionFmt to be set")
	}
	if m.config.RingBufferSize <= 0 {
		t.Error("expected default RingBufferSize to be set")
	}
}

func TestSessionCRUD_CreateAndList(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	// Create session
	if err := m.CreateSession("test-123"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// List sessions
	sessions, err := m.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].ID != "test-123" {
		t.Errorf("expected session ID 'test-123', got %q", sessions[0].ID)
	}
	if sessions[0].Status != StatusIdle {
		t.Errorf("expected status %q, got %q", StatusIdle, sessions[0].Status)
	}
}

func TestSessionCRUD_Delete(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.CreateSession("test-123")

	if err := m.DeleteSession("test-123"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	sessions, err := m.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions after delete, got %d", len(sessions))
	}
}

func TestSessionCRUD_DuplicateCreateIsIdempotent(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.CreateSession("dup-1")
	m.CreateSession("dup-1") // Should not error (INSERT OR IGNORE)

	sessions, _ := m.ListSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session after duplicate create, got %d", len(sessions))
	}
}

func TestGetMessages_Empty(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.CreateSession("msg-test")
	msgs, err := m.GetMessages("msg-test")
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(msgs))
	}
}

func TestGetMessages_FromDB(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.CreateSession("msg-test")
	now := nowUnix()
	m.db.Exec(`INSERT INTO messages (session_id, role, content, created_at) VALUES (?, 'user', 'hello', ?)`, "msg-test", now)
	m.db.Exec(`INSERT INTO messages (session_id, role, content, created_at) VALUES (?, 'assistant', 'hi', ?)`, "msg-test", now+1)

	msgs, err := m.GetMessages("msg-test")
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Errorf("unexpected first message: %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "hi" {
		t.Errorf("unexpected second message: %+v", msgs[1])
	}
}

func TestConversation_AppendAndRead(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.CreateSession("conv-test")
	m.appendConversation("conv-test", ConversationEntry{Role: "user", Content: "hello"})
	m.appendConversation("conv-test", ConversationEntry{Role: "assistant", Content: "hi there"})

	entries, err := m.GetConversation("conv-test")
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Role != "user" || entries[0].Content != "hello" {
		t.Errorf("unexpected first entry: %+v", entries[0])
	}
	if entries[1].Role != "assistant" || entries[1].Content != "hi there" {
		t.Errorf("unexpected second entry: %+v", entries[1])
	}
}

func TestSummary_WriteAndRead(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.CreateSession("sum-test")
	m.writeSummary("sum-test", SessionSummary{
		Status:  "idle",
		Summary: "test summary",
		Next:    "do something",
	})

	s := m.GetSummary("sum-test")
	if s.Summary != "test summary" {
		t.Errorf("expected summary 'test summary', got %q", s.Summary)
	}
	if s.Next != "do something" {
		t.Errorf("expected next 'do something', got %q", s.Next)
	}
}

func TestNormalizeContent_String(t *testing.T) {
	result := normalizeContent("hello")
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestNormalizeContent_Blocks(t *testing.T) {
	blocks := []interface{}{
		map[string]interface{}{"type": "text", "text": "part1"},
		map[string]interface{}{"type": "image", "url": "..."},
		map[string]interface{}{"type": "text", "text": "part2"},
	}
	result := normalizeContent(blocks)
	if result != "part1part2" {
		t.Errorf("expected 'part1part2', got %q", result)
	}
}

func TestNormalizeContent_Nil(t *testing.T) {
	result := normalizeContent(nil)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestGetMessages_PrefersConversationFile(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.CreateSession("pref-test")
	// Add to DB
	now := nowUnix()
	m.db.Exec(`INSERT INTO messages (session_id, role, content, created_at) VALUES (?, 'user', 'db-msg', ?)`, "pref-test", now)

	// Add to conversation file (should take precedence)
	m.appendConversation("pref-test", ConversationEntry{Role: "user", Content: "file-msg"})
	m.appendConversation("pref-test", ConversationEntry{Role: "assistant", Content: "file-reply"})

	msgs, err := m.GetMessages("pref-test")
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages from file, got %d", len(msgs))
	}
	if msgs[0].Content != "file-msg" {
		t.Errorf("expected file message, got %q", msgs[0].Content)
	}
}

func TestSessionDir_CreatesDirectory(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	dir := m.sessionDir("dir-test")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("expected session dir to be created: %s", dir)
	}
}

func TestQueueLength_Empty(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer m.Close()

	m.CreateSession("q-test")
	if n := m.QueueLength("q-test"); n != 0 {
		t.Errorf("expected queue length 0, got %d", n)
	}
}
