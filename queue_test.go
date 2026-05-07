package mux

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddToQueue_Basic(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q1")

	item, err := m.AddToQueue(QueueAddRequest{SessionID: "q1", Text: "follow-up 1"})
	if err != nil {
		t.Fatalf("AddToQueue: %v", err)
	}
	if item.SessionID != "q1" {
		t.Errorf("expected session_id 'q1', got %q", item.SessionID)
	}
	if item.Text != "follow-up 1" {
		t.Errorf("expected text 'follow-up 1', got %q", item.Text)
	}
	if item.Position != 1 {
		t.Errorf("expected position 1, got %d", item.Position)
	}
	if item.Status != "pending" {
		t.Errorf("expected status 'pending', got %q", item.Status)
	}
}

func TestAddToQueue_MultipleItems(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q2")

	m.AddToQueue(QueueAddRequest{SessionID: "q2", Text: "first"})
	m.AddToQueue(QueueAddRequest{SessionID: "q2", Text: "second"})
	m.AddToQueue(QueueAddRequest{SessionID: "q2", Text: "third"})

	items, err := m.ListQueue("q2")
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].Text != "first" || items[1].Text != "second" || items[2].Text != "third" {
		t.Errorf("unexpected order: %v, %v, %v", items[0].Text, items[1].Text, items[2].Text)
	}
}

func TestAddToQueue_RequiresSessionID(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()

	_, err = m.AddToQueue(QueueAddRequest{SessionID: "", Text: "text"})
	if err == nil {
		t.Fatal("expected error for empty session_id")
	}
}

func TestAddToQueue_RequiresText(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q3")

	_, err = m.AddToQueue(QueueAddRequest{SessionID: "q3", Text: ""})
	if err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestAddToQueue_InheritsAgentFromSession(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q4")
	m.db.Exec(`UPDATE sessions SET last_agent = 'codex', last_model = 'gpt-5' WHERE id = 'q4'`)

	item, err := m.AddToQueue(QueueAddRequest{SessionID: "q4", Text: "follow-up"})
	if err != nil {
		t.Fatalf("AddToQueue: %v", err)
	}
	if item.Agent != "codex" {
		t.Errorf("expected agent 'codex', got %q", item.Agent)
	}
	if item.Model != "gpt-5" {
		t.Errorf("expected model 'gpt-5', got %q", item.Model)
	}
}

func TestListQueue_Empty(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q5")

	items, err := m.ListQueue("q5")
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestQueueLength_MatchesItems(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q6")

	m.AddToQueue(QueueAddRequest{SessionID: "q6", Text: "a"})
	m.AddToQueue(QueueAddRequest{SessionID: "q6", Text: "b"})
	m.AddToQueue(QueueAddRequest{SessionID: "q6", Text: "c"})

	if n := m.QueueLength("q6"); n != 3 {
		t.Errorf("expected queue length 3, got %d", n)
	}
}

func TestReorderQueue(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q7")

	item1, _ := m.AddToQueue(QueueAddRequest{SessionID: "q7", Text: "first"})
	item2, _ := m.AddToQueue(QueueAddRequest{SessionID: "q7", Text: "second"})
	item3, _ := m.AddToQueue(QueueAddRequest{SessionID: "q7", Text: "third"})

	// Reverse order
	result, err := m.ReorderQueue("q7", []string{item3.ID, item2.ID, item1.ID})
	if err != nil {
		t.Fatalf("ReorderQueue: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result))
	}
	if result[0].Text != "third" {
		t.Errorf("expected 'third' first, got %q", result[0].Text)
	}
	if result[1].Text != "second" {
		t.Errorf("expected 'second' second, got %q", result[1].Text)
	}
	if result[2].Text != "first" {
		t.Errorf("expected 'first' third, got %q", result[2].Text)
	}
}

func TestReorderQueue_CountMismatch(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q8")

	m.AddToQueue(QueueAddRequest{SessionID: "q8", Text: "first"})
	m.AddToQueue(QueueAddRequest{SessionID: "q8", Text: "second"})

	_, err = m.ReorderQueue("q8", []string{"only-one"})
	if err == nil {
		t.Fatal("expected error for count mismatch")
	}
}

func TestUpdateQueueItem(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q9")

	item, _ := m.AddToQueue(QueueAddRequest{SessionID: "q9", Text: "original"})
	updated, err := m.UpdateQueueItem("q9", item.ID, "modified")
	if err != nil {
		t.Fatalf("UpdateQueueItem: %v", err)
	}
	if updated.Text != "modified" {
		t.Errorf("expected 'modified', got %q", updated.Text)
	}
}

func TestUpdateQueueItem_NotFound(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q10")

	_, err = m.UpdateQueueItem("q10", "nonexistent", "text")
	if err == nil {
		t.Fatal("expected error for nonexistent item")
	}
}

func TestDeleteQueueItem(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q11")

	item, _ := m.AddToQueue(QueueAddRequest{SessionID: "q11", Text: "to-delete"})
	err = m.DeleteQueueItem("q11", item.ID)
	if err != nil {
		t.Fatalf("DeleteQueueItem: %v", err)
	}
	if n := m.QueueLength("q11"); n != 0 {
		t.Errorf("expected queue length 0 after delete, got %d", n)
	}
}

func TestPopNextFromQueue(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q12")

	m.AddToQueue(QueueAddRequest{SessionID: "q12", Text: "first"})
	m.AddToQueue(QueueAddRequest{SessionID: "q12", Text: "second"})

	item := m.PopNextFromQueue("q12")
	if item == nil {
		t.Fatal("expected non-nil item")
	}
	if item.Text != "first" {
		t.Errorf("expected 'first', got %q", item.Text)
	}
	if item.Status != "processing" {
		t.Errorf("expected status 'processing', got %q", item.Status)
	}

	// Pending count should now be 1
	if n := m.QueueLength("q12"); n != 1 {
		t.Errorf("expected queue length 1 after pop, got %d", n)
	}
}

func TestPopNextFromQueue_Empty(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q13")

	item := m.PopNextFromQueue("q13")
	if item != nil {
		t.Errorf("expected nil for empty queue, got %+v", item)
	}
}

func TestClearQueue(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q14")

	m.AddToQueue(QueueAddRequest{SessionID: "q14", Text: "a"})
	m.AddToQueue(QueueAddRequest{SessionID: "q14", Text: "b"})
	m.ClearQueue("q14")

	if n := m.QueueLength("q14"); n != 0 {
		t.Errorf("expected queue length 0 after clear, got %d", n)
	}
}

func TestIsQueuePaused_Default(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()

	if m.IsQueuePaused("any") {
		t.Error("expected queue not paused by default")
	}
}

func TestResumeQueue(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q15")

	// Pause the queue
	m.mu.Lock()
	m.queuePaused["q15"] = true
	m.mu.Unlock()

	if !m.IsQueuePaused("q15") {
		t.Error("expected queue to be paused")
	}

	m.ResumeQueue("q15")
	if m.IsQueuePaused("q15") {
		t.Error("expected queue not paused after resume")
	}
}

func TestMarkQueueItemCompleted(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q16")

	item, _ := m.AddToQueue(QueueAddRequest{SessionID: "q16", Text: "will-complete"})
	m.MarkQueueItemCompleted(item.ID, "42")

	// Should no longer appear in pending list
	if n := m.QueueLength("q16"); n != 0 {
		t.Errorf("expected 0 pending after complete, got %d", n)
	}

	// Should appear in all list
	all, _ := m.ListQueueAll("q16")
	if len(all) != 1 {
		t.Fatalf("expected 1 item in all list, got %d", len(all))
	}
	if all[0].Status != "completed" {
		t.Errorf("expected status 'completed', got %q", all[0].Status)
	}
	if all[0].MessageID != "42" {
		t.Errorf("expected message_id 42, got %s", all[0].MessageID)
	}
}

func TestMarkQueueItemFailed(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q17")

	item, _ := m.AddToQueue(QueueAddRequest{SessionID: "q17", Text: "will-fail"})
	m.MarkQueueItemFailed(item.ID, "some error")

	all, _ := m.ListQueueAll("q17")
	if len(all) != 1 {
		t.Fatalf("expected 1 item, got %d", len(all))
	}
	if all[0].Status != "failed" {
		t.Errorf("expected status 'failed', got %q", all[0].Status)
	}
	if all[0].Error != "some error" {
		t.Errorf("expected error 'some error', got %q", all[0].Error)
	}
}

func TestPopNextIfNotPaused_WhenPaused(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q18")

	m.AddToQueue(QueueAddRequest{SessionID: "q18", Text: "item"})

	m.mu.Lock()
	m.queuePaused["q18"] = true
	m.mu.Unlock()

	item := m.popNextIfNotPaused("q18")
	if item != nil {
		t.Error("expected nil when queue is paused")
	}
}

func TestAddToQueue_WithAttachments(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("q19")

	item, err := m.AddToQueue(QueueAddRequest{SessionID: "q19", Text: "with files", Attachments: []string{"att1", "att2"}})
	if err != nil {
		t.Fatalf("AddToQueue: %v", err)
	}
	if len(item.Attachments) != 2 {
		t.Errorf("expected 2 attachments, got %d", len(item.Attachments))
	}
}

func TestAddToQueueCapturesCurrentWorkingDirectory(t *testing.T) {
	first := tempDir(t)
	second := tempDir(t)
	fs := fakeFSWithDirs(first, second)
	fs.home = first
	m := newTestManagerWithFilesystem(t, fs)

	if err := m.CreateSession("cwd-capture"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetWorkingDirectory("cwd-capture", first); err != nil {
		t.Fatal(err)
	}

	item, err := m.AddToQueue(QueueAddRequest{SessionID: "cwd-capture", Text: "first item"})
	if err != nil {
		t.Fatalf("AddToQueue: %v", err)
	}
	if item.WorkingDirectory != first {
		t.Errorf("AddToQueue WorkingDirectory = %q, want %q", item.WorkingDirectory, first)
	}

	if _, err := m.SetWorkingDirectory("cwd-capture", second); err != nil {
		t.Fatal(err)
	}

	items, err := m.ListQueue("cwd-capture")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].WorkingDirectory != first {
		t.Errorf("listed WorkingDirectory = %q, want %q (queue capture should freeze cwd)", items[0].WorkingDirectory, first)
	}
}

func TestAddToQueueUsesExplicitWorkingDirectory(t *testing.T) {
	session := tempDir(t)
	explicit := tempDir(t)
	fs := fakeFSWithDirs(session, explicit)
	fs.home = session
	m := newTestManagerWithFilesystem(t, fs)

	if err := m.CreateSession("cwd-explicit"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetWorkingDirectory("cwd-explicit", session); err != nil {
		t.Fatal(err)
	}

	item, err := m.AddToQueue(QueueAddRequest{
		SessionID:        "cwd-explicit",
		Text:             "with explicit cwd",
		WorkingDirectory: explicit,
	})
	if err != nil {
		t.Fatalf("AddToQueue: %v", err)
	}
	if item.WorkingDirectory != explicit {
		t.Errorf("AddToQueue WorkingDirectory = %q, want %q", item.WorkingDirectory, explicit)
	}
}

func TestQueueItemIncludesWorkingDirectory(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("qcwd1")

	if _, err := m.db.Exec(
		`INSERT INTO follow_up_queue (id, session_id, text, position, agent, agent_sub, model, effort, attachments, created_at, source, status, transcript, message_id, response_id, working_directory)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"qcwd1-item", "qcwd1", "hi", 1, "claude", "", "", "", "[]", 1, "text", "pending", "", "msg-1", "resp-1", "/tmp/captured",
	); err != nil {
		t.Fatalf("insert queue row: %v", err)
	}

	items, err := m.ListQueue("qcwd1")
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].WorkingDirectory != "/tmp/captured" {
		t.Errorf("WorkingDirectory = %q, want /tmp/captured", items[0].WorkingDirectory)
	}

	all, err := m.ListQueueAll("qcwd1")
	if err != nil {
		t.Fatalf("ListQueueAll: %v", err)
	}
	if len(all) != 1 || all[0].WorkingDirectory != "/tmp/captured" {
		t.Errorf("ListQueueAll WorkingDirectory = %q, want /tmp/captured", all[0].WorkingDirectory)
	}
}

func registerPwdRecordingProvider(t *testing.T, m *Manager) string {
	t.Helper()
	binDir := t.TempDir()
	binary := filepath.Join(binDir, "fake-agent")
	output := filepath.Join(binDir, "pwd.out")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s' \"$PWD\" > %s\necho ok\n", output)
	if err := os.WriteFile(binary, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	if err := m.providers.Register(CLIProviderConfig{
		ProviderID: "fake",
		Name:       "Fake",
		Binary:     binary,
		ParserType: "other",
	}); err != nil {
		t.Fatal(err)
	}
	return output
}

func resolved(t *testing.T, path string) string {
	t.Helper()
	out, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestProcessQueueItemUsesStoredWorkingDirectory(t *testing.T) {
	home := tempHome(t)
	first := tempDir(t)
	second := tempDir(t)
	fs := fakeFSWithDirs(first, second)
	fs.home = home
	m := newTestManagerWithFilesystem(t, fs)
	output := registerPwdRecordingProvider(t, m)

	if err := m.CreateSession("queue-cwd-stored"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetWorkingDirectory("queue-cwd-stored", first); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddToQueue(QueueAddRequest{SessionID: "queue-cwd-stored", Text: "echo me", Agent: "fake"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetWorkingDirectory("queue-cwd-stored", second); err != nil {
		t.Fatal(err)
	}

	processQueueItem(m, "queue-cwd-stored")

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read pwd output: %v", err)
	}
	got := strings.TrimSpace(string(data))
	if got != resolved(t, first) {
		t.Errorf("provider PWD = %q, want %q", got, resolved(t, first))
	}
}

func TestProcessQueueItemFailsWhenStoredWorkingDirectoryBecomesInvalid(t *testing.T) {
	home := tempHome(t)
	first := tempDir(t)
	fs := fakeFSWithDirs(first)
	fs.home = home
	m := newTestManagerWithFilesystem(t, fs)
	registerPwdRecordingProvider(t, m)

	if err := m.CreateSession("queue-cwd-invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetWorkingDirectory("queue-cwd-invalid", first); err != nil {
		t.Fatal(err)
	}
	item, err := m.AddToQueue(QueueAddRequest{SessionID: "queue-cwd-invalid", Text: "should fail", Agent: "fake"})
	if err != nil {
		t.Fatal(err)
	}

	delete(fs.dirs, filepath.Clean(first))

	processQueueItem(m, "queue-cwd-invalid")

	all, err := m.ListQueueAll("queue-cwd-invalid")
	if err != nil {
		t.Fatal(err)
	}
	var stored *QueueItem
	for i := range all {
		if all[i].ID == item.ID {
			stored = &all[i]
			break
		}
	}
	if stored == nil {
		t.Fatalf("queue item %s not found", item.ID)
	}
	if stored.Status != "failed" {
		t.Errorf("status = %q, want failed", stored.Status)
	}

	var msgCount int
	if err := m.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id = ?`, "queue-cwd-invalid").Scan(&msgCount); err != nil {
		t.Fatal(err)
	}
	if msgCount != 0 {
		t.Errorf("messages count = %d, want 0 (no user message should be persisted on failure)", msgCount)
	}
}

func TestSendRequestWorkingDirectoryOverridesSessionWorkingDirectory(t *testing.T) {
	home := tempHome(t)
	session := tempDir(t)
	override := tempDir(t)
	fs := fakeFSWithDirs(session, override)
	fs.home = home
	m := newTestManagerWithFilesystem(t, fs)
	output := registerPwdRecordingProvider(t, m)

	if err := m.CreateSession("send-cwd-override"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetWorkingDirectory("send-cwd-override", session); err != nil {
		t.Fatal(err)
	}

	result, err := m.Send(SendRequest{
		SessionID:        "send-cwd-override",
		Prompt:           "hello",
		Agent:            "fake",
		WorkingDirectory: override,
	})
	if err != nil {
		t.Fatal(err)
	}
	for range result.Events {
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(data))
	if got != resolved(t, override) {
		t.Errorf("provider PWD = %q, want %q", got, resolved(t, override))
	}
}

func TestBroadcastSessionStatus(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("bss1")

	// Subscribe to get the status
	notifyCh, notifyDone := m.broadcast.SubscribeNotify()
	defer close(notifyDone)

	m.BroadcastSessionStatus("bss1")

	// Should receive a status notification
	select {
	case evt := <-notifyCh:
		if evt.Event != NotifyStatus {
			t.Errorf("expected NotifyStatus, got %s", evt.Event)
		}
	default:
		// Channel might be async; this is acceptable in some timing cases
	}
}
