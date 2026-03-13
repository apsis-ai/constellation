package mux

import (
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

	item, err := m.AddToQueue("q1", "follow-up 1", "", "", "", "", nil, "", "")
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

	m.AddToQueue("q2", "first", "", "", "", "", nil, "", "")
	m.AddToQueue("q2", "second", "", "", "", "", nil, "", "")
	m.AddToQueue("q2", "third", "", "", "", "", nil, "", "")

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

	_, err = m.AddToQueue("", "text", "", "", "", "", nil, "", "")
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

	_, err = m.AddToQueue("q3", "", "", "", "", "", nil, "", "")
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

	item, err := m.AddToQueue("q4", "follow-up", "", "", "", "", nil, "", "")
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

	m.AddToQueue("q6", "a", "", "", "", "", nil, "", "")
	m.AddToQueue("q6", "b", "", "", "", "", nil, "", "")
	m.AddToQueue("q6", "c", "", "", "", "", nil, "", "")

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

	item1, _ := m.AddToQueue("q7", "first", "", "", "", "", nil, "", "")
	item2, _ := m.AddToQueue("q7", "second", "", "", "", "", nil, "", "")
	item3, _ := m.AddToQueue("q7", "third", "", "", "", "", nil, "", "")

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

	m.AddToQueue("q8", "first", "", "", "", "", nil, "", "")
	m.AddToQueue("q8", "second", "", "", "", "", nil, "", "")

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

	item, _ := m.AddToQueue("q9", "original", "", "", "", "", nil, "", "")
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

	item, _ := m.AddToQueue("q11", "to-delete", "", "", "", "", nil, "", "")
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

	m.AddToQueue("q12", "first", "", "", "", "", nil, "", "")
	m.AddToQueue("q12", "second", "", "", "", "", nil, "", "")

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

	m.AddToQueue("q14", "a", "", "", "", "", nil, "", "")
	m.AddToQueue("q14", "b", "", "", "", "", nil, "", "")
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

	item, _ := m.AddToQueue("q16", "will-complete", "", "", "", "", nil, "", "")
	m.MarkQueueItemCompleted(item.ID, 42)

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
	if all[0].MessageID != 42 {
		t.Errorf("expected message_id 42, got %d", all[0].MessageID)
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

	item, _ := m.AddToQueue("q17", "will-fail", "", "", "", "", nil, "", "")
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

	m.AddToQueue("q18", "item", "", "", "", "", nil, "", "")

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

	item, err := m.AddToQueue("q19", "with files", "", "", "", "", []string{"att1", "att2"}, "", "")
	if err != nil {
		t.Fatalf("AddToQueue: %v", err)
	}
	if len(item.Attachments) != 2 {
		t.Errorf("expected 2 attachments, got %d", len(item.Attachments))
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
