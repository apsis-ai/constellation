package mux

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHandleHandoff_WritesFile(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("h1")

	err = m.HandleHandoff("h1", "summary text", "current state", "pending tasks")
	if err != nil {
		t.Fatalf("HandleHandoff: %v", err)
	}

	// Verify handoff file was written
	entries, _ := os.ReadDir(cfg.HandoffDir)
	if len(entries) == 0 {
		t.Fatal("expected handoff file to be created")
	}
	data, _ := os.ReadFile(filepath.Join(cfg.HandoffDir, entries[0].Name()))
	content := string(data)
	if !stringContains(content, "summary text") {
		t.Error("handoff file missing summary")
	}
	if !stringContains(content, "current state") {
		t.Error("handoff file missing current state")
	}
	if !stringContains(content, "pending tasks") {
		t.Error("handoff file missing pending tasks")
	}
}

func TestHandleHandoff_UpdatesDB(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("h2")

	m.HandleHandoff("h2", "sum", "state", "tasks")

	var status string
	var handoffPath string
	m.db.QueryRow(`SELECT status, COALESCE(handoff_path,'') FROM sessions WHERE id = 'h2'`).Scan(&status, &handoffPath)
	if status != string(StatusIdle) {
		t.Errorf("expected status idle, got %q", status)
	}
	if handoffPath == "" {
		t.Error("expected handoff_path to be set")
	}
}

func TestHandleHandoff_RequiresSessionID(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()

	err = m.HandleHandoff("", "sum", "state", "tasks")
	if err == nil {
		t.Fatal("expected error for empty session_id")
	}
}

func TestHandleHandoff_UsesHandlerInterface(t *testing.T) {
	var called bool
	handler := &mockHandoffHandler{fn: func(sessionID, summary, state, tasks string) error {
		called = true
		if sessionID != "h3" {
			t.Errorf("expected 'h3', got %q", sessionID)
		}
		return nil
	}}
	cfg := tempConfig(t)
	cfg.HandoffHdl = handler
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("h3")

	m.HandleHandoff("h3", "sum", "state", "tasks")
	if !called {
		t.Error("expected HandoffHandler to be called")
	}
}

type mockHandoffHandler struct {
	fn func(sessionID, summary, state, tasks string) error
}

func (h *mockHandoffHandler) HandleHandoff(sessionID, summary, state, tasks string) error {
	return h.fn(sessionID, summary, state, tasks)
}

func TestResetIdleTimer_FiresTriggerHandoff(t *testing.T) {
	cfg := tempConfig(t)
	cfg.IdleTimeout = 50 * time.Millisecond
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("idle1")

	m.resetIdleTimer("idle1")

	// Wait for timer to fire
	time.Sleep(150 * time.Millisecond)

	// Verify the idle entry was cleaned up (triggerHandoff clears it)
	m.mu.Lock()
	_, exists := m.idleMap["idle1"]
	m.mu.Unlock()
	if exists {
		t.Error("expected idle entry to be cleaned up after timeout")
	}
}

func TestResetIdleTimer_ResetsOnSubsequentCalls(t *testing.T) {
	cfg := tempConfig(t)
	cfg.IdleTimeout = 100 * time.Millisecond
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("idle2")

	m.resetIdleTimer("idle2")
	time.Sleep(50 * time.Millisecond)
	m.resetIdleTimer("idle2") // Reset, should delay another 100ms
	time.Sleep(50 * time.Millisecond)

	// After 100ms total, timer shouldn't have fired yet (was reset at 50ms)
	m.mu.Lock()
	_, exists := m.idleMap["idle2"]
	m.mu.Unlock()
	if !exists {
		t.Error("expected idle entry to still exist (timer was reset)")
	}
}

func TestStop_CleansUpState(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("stop1")

	// Simulate an active process by adding to activeProcesses
	m.mu.Lock()
	m.activeProcesses["stop1"] = &processEntry{
		Pid:  99999, // Non-existent PID
		Kill: func() error { return fmt.Errorf("mock kill") },
	}
	m.mu.Unlock()

	err = m.Stop("stop1")
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Should be marked as stopped
	m.mu.Lock()
	_, hasProcess := m.activeProcesses["stop1"]
	isPaused := m.queuePaused["stop1"]
	m.mu.Unlock()

	if hasProcess {
		t.Error("expected process to be removed from activeProcesses")
	}
	if !isPaused {
		t.Error("expected queue to be paused after stop")
	}

	// DB should show idle
	var status string
	m.db.QueryRow(`SELECT status FROM sessions WHERE id = 'stop1'`).Scan(&status)
	if status != string(StatusIdle) {
		t.Errorf("expected status idle, got %q", status)
	}
}

func TestStop_NoProcess(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("stop2")

	// Stop with no active process should not error
	err = m.Stop("stop2")
	if err != nil {
		t.Fatalf("Stop (no process): %v", err)
	}
}

func TestStop_CancelsIdleTimer(t *testing.T) {
	cfg := tempConfig(t)
	cfg.IdleTimeout = 10 * time.Second
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("stop3")

	m.resetIdleTimer("stop3")
	m.Stop("stop3")

	m.mu.Lock()
	_, exists := m.idleMap["stop3"]
	m.mu.Unlock()
	if exists {
		t.Error("expected idle timer to be cleared after Stop")
	}
}

func TestStopAll(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("sa1")
	m.CreateSession("sa2")

	m.mu.Lock()
	m.activeProcesses["sa1"] = &processEntry{Pid: 99998, Kill: func() error { return nil }}
	m.activeProcesses["sa2"] = &processEntry{Pid: 99997, Kill: func() error { return nil }}
	m.mu.Unlock()

	errs := m.StopAll()
	// Errors from killing non-existent PIDs are expected
	_ = errs

	m.mu.Lock()
	processCount := len(m.activeProcesses)
	m.mu.Unlock()

	if processCount != 0 {
		t.Errorf("expected 0 active processes, got %d", processCount)
	}

	// All sessions should be idle
	var status string
	m.db.QueryRow(`SELECT status FROM sessions WHERE id = 'sa1'`).Scan(&status)
	if status != string(StatusIdle) {
		t.Errorf("expected sa1 status idle, got %q", status)
	}
}

func TestSetAskUserPending(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()

	m.SetAskUserPending("ask1", "What color?", []string{"red", "blue"})

	m.mu.Lock()
	pending, ok := m.askUserPending["ask1"]
	m.mu.Unlock()

	if !ok {
		t.Fatal("expected ask_user pending to be set")
	}
	if pending.Question != "What color?" {
		t.Errorf("expected question 'What color?', got %q", pending.Question)
	}
	if len(pending.Options) != 2 {
		t.Errorf("expected 2 options, got %d", len(pending.Options))
	}
}

func TestSetAskUserPending_DoesNotOverwrite(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()

	m.SetAskUserPending("ask2", "First question", nil)
	m.SetAskUserPending("ask2", "Second question", nil)

	m.mu.Lock()
	pending := m.askUserPending["ask2"]
	m.mu.Unlock()

	if pending.Question != "First question" {
		t.Errorf("expected first question to be kept, got %q", pending.Question)
	}
}

func TestSkipAsk_ClearsPending(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("skip1")

	// Set status to waiting
	m.db.Exec(`UPDATE sessions SET status = 'waiting' WHERE id = 'skip1'`)
	m.SetAskUserPending("skip1", "Are you sure?", nil)

	err = m.SkipAsk("skip1")
	if err != nil {
		t.Fatalf("SkipAsk: %v", err)
	}

	m.mu.Lock()
	_, exists := m.askUserPending["skip1"]
	m.mu.Unlock()
	if exists {
		t.Error("expected askUserPending to be cleared")
	}

	var status string
	m.db.QueryRow(`SELECT status FROM sessions WHERE id = 'skip1'`).Scan(&status)
	if status != "idle" {
		t.Errorf("expected status 'idle', got %q", status)
	}
}

func TestSkipAsk_RequiresWaitingStatus(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("skip2")

	err = m.SkipAsk("skip2")
	if err == nil {
		t.Fatal("expected error when session is not waiting")
	}
}

func TestTrackScreenshot(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()
	m.CreateSession("ss1")

	err = m.TrackScreenshot("ss1")
	if err != nil {
		t.Fatalf("TrackScreenshot: %v", err)
	}

	var count int
	m.db.QueryRow(`SELECT screenshot_count FROM sessions WHERE id = 'ss1'`).Scan(&count)
	if count != 1 {
		t.Errorf("expected screenshot_count 1, got %d", count)
	}
}

func TestTrackScreenshot_RequiresSessionID(t *testing.T) {
	cfg := tempConfig(t)
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()

	err = m.TrackScreenshot("")
	if err == nil {
		t.Fatal("expected error for empty session_id")
	}
}

func stringContains(s, sub string) bool {
	return len(s) >= len(sub) && searchStr(s, sub)
}

func searchStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
