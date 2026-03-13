package mux

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// screenshotHandoffThreshold is the number of screenshots before auto-handoff.
const screenshotHandoffThreshold = 20

// HandleHandoff persists the handoff state for a session.
// If a HandoffHandler is configured, it delegates to that; otherwise writes a markdown file.
func (m *Manager) HandleHandoff(sessionID, summary, currentState, pendingTasks string) error {
	if sessionID == "" {
		return fmt.Errorf("session_id required")
	}

	if m.config.HandoffHdl != nil {
		if err := m.config.HandoffHdl.HandleHandoff(sessionID, summary, currentState, pendingTasks); err != nil {
			return err
		}
	}

	now := time.Now()
	filename := fmt.Sprintf("%s_%d.md", sessionID, now.Unix())
	path := filepath.Join(m.config.HandoffDir, filename)
	body := fmt.Sprintf("# Handoff\n\n## Summary\n%s\n\n## Current state\n%s\n\n## Pending tasks\n%s\n",
		summary, currentState, pendingTasks)
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		return fmt.Errorf("write handoff file: %w", err)
	}
	ts := now.Unix()
	_, err := m.db.Exec(`UPDATE sessions SET handoff_path = ?, last_active_at = ?, status = ?, conversation_id = NULL, screenshot_count = 0 WHERE id = ?`,
		path, ts, StatusIdle, sessionID)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	return nil
}

// Stop kills the active process for a session, if any.
func (m *Manager) Stop(sessionID string) error {
	m.mu.Lock()
	proc, ok := m.activeProcesses[sessionID]
	if ok {
		delete(m.activeProcesses, sessionID)
	}
	// Mark as explicitly stopped so the cleanup goroutine skips handoff/timer.
	m.stoppedSessions[sessionID] = true
	m.queuePaused[sessionID] = true
	if entry, has := m.idleMap[sessionID]; has {
		entry.mu.Lock()
		if entry.timer != nil {
			entry.timer.Stop()
		}
		entry.mu.Unlock()
		delete(m.idleMap, sessionID)
	}
	m.mu.Unlock()

	if !ok {
		// No in-memory process — check DB for a persisted PID (orphaned process).
		var pid int
		_ = m.db.QueryRow(`SELECT pid FROM sessions WHERE id = ?`, sessionID).Scan(&pid)
		if pid > 0 {
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
		_, _ = m.db.Exec(`UPDATE sessions SET status = ?, pid = 0 WHERE id = ?`, StatusIdle, sessionID)
		m.broadcast.PublishDone(sessionID, "")
		m.broadcast.PublishStatus(sessionID, "idle", "", "", "", m.QueueLength(sessionID), m.IsQueuePaused(sessionID))
		return nil
	}

	// Kill entire process group so child processes (MCP servers) also die.
	if proc.Kill != nil {
		_ = proc.Kill()
	}
	_, _ = m.db.Exec(`UPDATE sessions SET status = ?, pid = 0 WHERE id = ?`, StatusIdle, sessionID)
	m.broadcast.PublishDone(sessionID, "")
	m.broadcast.PublishStatus(sessionID, "idle", "", "", "", m.QueueLength(sessionID), m.IsQueuePaused(sessionID))
	return nil
}

// StopAll kills all active processes and marks all active sessions as idle.
func (m *Manager) StopAll() []error {
	m.mu.Lock()
	ids := make([]string, 0, len(m.activeProcesses))
	procs := make([]*processEntry, 0, len(m.activeProcesses))
	for id, proc := range m.activeProcesses {
		ids = append(ids, id)
		procs = append(procs, proc)
	}
	// Clear all active processes and idle timers; mark all as explicitly stopped and paused.
	for _, id := range ids {
		delete(m.activeProcesses, id)
		m.stoppedSessions[id] = true
		m.queuePaused[id] = true
	}
	for id, entry := range m.idleMap {
		entry.mu.Lock()
		if entry.timer != nil {
			entry.timer.Stop()
		}
		entry.mu.Unlock()
		delete(m.idleMap, id)
		m.stoppedSessions[id] = true
		m.queuePaused[id] = true
	}
	m.mu.Unlock()

	var errs []error
	for i, proc := range procs {
		if proc.Kill != nil {
			if err := proc.Kill(); err != nil {
				errs = append(errs, fmt.Errorf("kill session %s: %w", ids[i], err))
			}
		}
	}

	// Also kill any orphaned processes tracked only in the DB.
	inMemoryPIDs := make(map[int]bool, len(procs))
	for _, proc := range procs {
		inMemoryPIDs[proc.Pid] = true
	}
	rows, err := m.db.Query(`SELECT id, pid FROM sessions WHERE pid > 0`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var sid string
			var pid int
			if rows.Scan(&sid, &pid) == nil && !inMemoryPIDs[pid] {
				if killErr := syscall.Kill(-pid, syscall.SIGKILL); killErr != nil {
					errs = append(errs, fmt.Errorf("kill orphaned session %s (pid %d): %w", sid, pid, killErr))
				}
			}
		}
	}

	// Mark all active sessions as idle and clear PIDs in the DB.
	_, _ = m.db.Exec(`UPDATE sessions SET status = ?, pid = 0 WHERE status = ? OR pid > 0`, StatusIdle, StatusActive)

	// Broadcast terminal events so SSE clients clear sending state.
	for _, id := range ids {
		m.broadcast.PublishDone(id, "")
		m.broadcast.PublishStatus(id, "idle", "", "", "", m.QueueLength(id), m.IsQueuePaused(id))
	}
	return errs
}

// TrackScreenshot increments the screenshot count for a session.
func (m *Manager) TrackScreenshot(sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session_id required")
	}
	var count int
	if err := m.db.QueryRow(
		`UPDATE sessions SET screenshot_count = screenshot_count + 1, last_active_at = ? WHERE id = ? RETURNING screenshot_count`,
		time.Now().Unix(), sessionID,
	).Scan(&count); err != nil {
		return fmt.Errorf("track screenshot: %w", err)
	}
	if count >= screenshotHandoffThreshold {
		go m.triggerHandoff(sessionID)
	}
	return nil
}
