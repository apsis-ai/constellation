package mux

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
)

// AddToQueue inserts a follow-up message into the queue for a session.
func (m *Manager) AddToQueue(sessionID string, text string, agent, agentSub, model, effort string, attachments []string, source, transcript string) (*QueueItem, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session_id required")
	}
	if text == "" {
		return nil, fmt.Errorf("text required")
	}
	if source == "" {
		source = "text"
	}

	now := time.Now().Unix()
	_, _ = m.db.Exec(`INSERT OR IGNORE INTO sessions (id, status, handoff_path, conversation_id, token_usage, created_at, last_active_at)
		VALUES (?, ?, '', NULL, NULL, ?, ?)`, sessionID, StatusActive, now, now)

	var lastAgent, lastAgentSub, lastModel, lastEffort string
	_ = m.db.QueryRow(`SELECT COALESCE(last_agent,'claude'), COALESCE(last_agent_sub,''), COALESCE(last_model,''), COALESCE(last_effort,'') FROM sessions WHERE id = ?`, sessionID).Scan(&lastAgent, &lastAgentSub, &lastModel, &lastEffort)
	if lastAgent == "" {
		lastAgent = "claude"
	}
	if agent == "" {
		agent = lastAgent
	}
	if agentSub == "" {
		agentSub = lastAgentSub
	}
	if model == "" {
		model = lastModel
	}
	if effort == "" {
		effort = lastEffort
	}

	var maxPos sql.NullInt64
	_ = m.db.QueryRow(`SELECT MAX(position) FROM follow_up_queue WHERE session_id = ? AND status = 'pending'`, sessionID).Scan(&maxPos)
	position := 1
	if maxPos.Valid {
		position = int(maxPos.Int64) + 1
	}

	id := uuid.New().String()
	attJSON := "[]"
	if len(attachments) > 0 {
		b, _ := json.Marshal(attachments)
		attJSON = string(b)
	}

	_, err := m.db.Exec(`INSERT INTO follow_up_queue (id, session_id, text, position, agent, agent_sub, model, effort, attachments, created_at, source, status, transcript)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?)`,
		id, sessionID, text, position, agent, agentSub, model, effort, attJSON, now, source, transcript)
	if err != nil {
		return nil, err
	}

	return &QueueItem{
		ID:          id,
		SessionID:   sessionID,
		Text:        text,
		Position:    position,
		Agent:       agent,
		AgentSub:    agentSub,
		Model:       model,
		Effort:      effort,
		Attachments: attachments,
		CreatedAt:   now,
		Source:      source,
		Status:      "pending",
		Transcript:  transcript,
	}, nil
}

// ListQueue returns pending queue items for a session ordered by position ascending.
func (m *Manager) ListQueue(sessionID string) ([]QueueItem, error) {
	rows, err := m.db.Query(`SELECT id, session_id, text, position, agent, agent_sub, model, effort, COALESCE(attachments,'[]'), created_at, source, status, transcript, message_id, started_at, completed_at, error
		FROM follow_up_queue WHERE session_id = ? AND status = 'pending' ORDER BY position ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []QueueItem
	for rows.Next() {
		var q QueueItem
		var attJSON string
		if err := rows.Scan(&q.ID, &q.SessionID, &q.Text, &q.Position, &q.Agent, &q.AgentSub, &q.Model, &q.Effort, &attJSON, &q.CreatedAt, &q.Source, &q.Status, &q.Transcript, &q.MessageID, &q.StartedAt, &q.CompletedAt, &q.Error); err != nil {
			return nil, err
		}
		if attJSON != "" && attJSON != "[]" {
			_ = json.Unmarshal([]byte(attJSON), &q.Attachments)
		}
		items = append(items, q)
	}
	return items, nil
}

// ListQueueAll returns all queue items for a session (including completed/failed) ordered by position ascending.
func (m *Manager) ListQueueAll(sessionID string) ([]QueueItem, error) {
	rows, err := m.db.Query(`SELECT id, session_id, text, position, agent, agent_sub, model, effort, COALESCE(attachments,'[]'), created_at, source, status, transcript, message_id, started_at, completed_at, error
		FROM follow_up_queue WHERE session_id = ? ORDER BY position ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []QueueItem
	for rows.Next() {
		var q QueueItem
		var attJSON string
		if err := rows.Scan(&q.ID, &q.SessionID, &q.Text, &q.Position, &q.Agent, &q.AgentSub, &q.Model, &q.Effort, &attJSON, &q.CreatedAt, &q.Source, &q.Status, &q.Transcript, &q.MessageID, &q.StartedAt, &q.CompletedAt, &q.Error); err != nil {
			return nil, err
		}
		if attJSON != "" && attJSON != "[]" {
			_ = json.Unmarshal([]byte(attJSON), &q.Attachments)
		}
		items = append(items, q)
	}
	return items, nil
}

// UpdateQueueItem updates a queued item's text. Returns error if item not found.
func (m *Manager) UpdateQueueItem(sessionID, itemID string, text string) (*QueueItem, error) {
	res, err := m.db.Exec(`UPDATE follow_up_queue SET text = ? WHERE id = ? AND session_id = ? AND status = 'pending'`, text, itemID, sessionID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, fmt.Errorf("queue item %s not found or already processed", itemID)
	}
	var q QueueItem
	var attJSON string
	err = m.db.QueryRow(`SELECT id, session_id, text, position, agent, agent_sub, model, effort, COALESCE(attachments,'[]'), created_at, source, status, transcript, message_id, started_at, completed_at, error
		FROM follow_up_queue WHERE id = ?`, itemID).Scan(&q.ID, &q.SessionID, &q.Text, &q.Position, &q.Agent, &q.AgentSub, &q.Model, &q.Effort, &attJSON, &q.CreatedAt, &q.Source, &q.Status, &q.Transcript, &q.MessageID, &q.StartedAt, &q.CompletedAt, &q.Error)
	if err != nil {
		return nil, err
	}
	if attJSON != "" && attJSON != "[]" {
		_ = json.Unmarshal([]byte(attJSON), &q.Attachments)
	}
	return &q, nil
}

// DeleteQueueItem removes a queue item. Idempotent.
func (m *Manager) DeleteQueueItem(sessionID, itemID string) error {
	_, _ = m.db.Exec(`DELETE FROM follow_up_queue WHERE id = ? AND session_id = ?`, itemID, sessionID)
	return nil
}

// PopNextFromQueue atomically claims the lowest-position pending item as 'processing' and returns it.
func (m *Manager) PopNextFromQueue(sessionID string) *QueueItem {
	tx, err := m.db.Begin()
	if err != nil {
		return nil
	}
	defer tx.Rollback()

	var q QueueItem
	var attJSON string
	err = tx.QueryRow(`SELECT id, session_id, text, position, agent, agent_sub, model, effort, attachments, created_at, source, transcript
		FROM follow_up_queue WHERE session_id = ? AND status = 'pending' ORDER BY position ASC LIMIT 1`, sessionID).Scan(
		&q.ID, &q.SessionID, &q.Text, &q.Position, &q.Agent, &q.AgentSub, &q.Model, &q.Effort, &attJSON, &q.CreatedAt, &q.Source, &q.Transcript)
	if err != nil {
		return nil
	}

	now := time.Now().Unix()
	res, _ := tx.Exec(`UPDATE follow_up_queue SET status = 'processing', started_at = ? WHERE id = ? AND status = 'pending'`, now, q.ID)
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}

	if err := tx.Commit(); err != nil {
		return nil
	}

	q.Status = "processing"
	q.StartedAt = now
	if attJSON != "" && attJSON != "[]" {
		_ = json.Unmarshal([]byte(attJSON), &q.Attachments)
	}
	return &q
}

// ClearQueue removes all pending queue items for a session.
func (m *Manager) ClearQueue(sessionID string) {
	_, _ = m.db.Exec(`DELETE FROM follow_up_queue WHERE session_id = ? AND status = 'pending'`, sessionID)
}

// MarkQueueItemCompleted marks a queue item as completed with the linked message ID.
func (m *Manager) MarkQueueItemCompleted(itemID string, messageID int64) {
	now := time.Now().Unix()
	_, _ = m.db.Exec(`UPDATE follow_up_queue SET status = 'completed', message_id = ?, completed_at = ? WHERE id = ?`,
		messageID, now, itemID)
}

// MarkQueueItemFailed marks a queue item as failed with an error message.
func (m *Manager) MarkQueueItemFailed(itemID string, errMsg string) {
	now := time.Now().Unix()
	_, _ = m.db.Exec(`UPDATE follow_up_queue SET status = 'failed', error = ?, completed_at = ? WHERE id = ?`,
		errMsg, now, itemID)
}

// ReorderQueue sets positions based on the order of IDs provided.
func (m *Manager) ReorderQueue(sessionID string, orderedIDs []string) ([]QueueItem, error) {
	var count int
	_ = m.db.QueryRow(`SELECT COUNT(*) FROM follow_up_queue WHERE session_id = ? AND status = 'pending'`, sessionID).Scan(&count)
	if count != len(orderedIDs) {
		return nil, fmt.Errorf("reorder conflict: expected %d items but got %d (queue may have changed)", count, len(orderedIDs))
	}

	tx, err := m.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	for i, id := range orderedIDs {
		res, err := tx.Exec(`UPDATE follow_up_queue SET position = ? WHERE id = ? AND session_id = ? AND status = 'pending'`,
			i+1, id, sessionID)
		if err != nil {
			return nil, err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return nil, fmt.Errorf("reorder conflict: item %s not found in pending queue", id)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return m.ListQueue(sessionID)
}

// IsQueuePaused returns whether the queue is paused for a session.
func (m *Manager) IsQueuePaused(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.queuePaused == nil {
		return false
	}
	return m.queuePaused[sessionID]
}

// ResumeQueue clears the pause flag and processes the next queued item.
func (m *Manager) ResumeQueue(sessionID string) {
	m.mu.Lock()
	delete(m.queuePaused, sessionID)
	m.mu.Unlock()
	go m.ProcessNextFromQueue(sessionID)
}

// popNextIfNotPaused atomically checks pause state and pops under a single lock.
func (m *Manager) popNextIfNotPaused(sessionID string) *QueueItem {
	m.mu.Lock()
	if m.queuePaused[sessionID] {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	return m.PopNextFromQueue(sessionID)
}

// BroadcastSessionStatus sends the current session status to all SSE subscribers.
func (m *Manager) BroadcastSessionStatus(sessionID string) {
	st := m.GetSessionStatus(sessionID)
	m.broadcast.PublishStatus(sessionID, st.Status, st.Summary, st.Tool, m.getUserMessage(sessionID), st.QueueLength, m.IsQueuePaused(sessionID))
}

// SetAskUserPending stores a question for the user.
func (m *Manager) SetAskUserPending(sessionID string, question string, options []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.askUserPending[sessionID]; !exists {
		m.askUserPending[sessionID] = AskUserPending{Question: question, Options: options}
	}
}

// SkipAsk clears a pending ask_user question without feeding an answer to the AI.
func (m *Manager) SkipAsk(sessionID string) error {
	var dbStatus string
	err := m.db.QueryRow(`SELECT status FROM sessions WHERE id = ?`, sessionID).Scan(&dbStatus)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}
	if dbStatus != "waiting" {
		return fmt.Errorf("session is not waiting for user input")
	}

	m.mu.Lock()
	delete(m.askUserPending, sessionID)
	m.mu.Unlock()

	_, _ = m.db.Exec(`UPDATE sessions SET status = 'idle' WHERE id = ?`, sessionID)
	m.broadcast.PublishStatus(sessionID, "idle", "", "", m.getUserMessage(sessionID), m.QueueLength(sessionID), m.IsQueuePaused(sessionID))
	go m.ProcessNextFromQueue(sessionID)
	return nil
}

// processQueueItem pops the next queue item and processes it via Send.
// This is the real implementation replacing the Phase 1 stub.
func processQueueItem(m *Manager, sessionID string) {
	item := m.popNextIfNotPaused(sessionID)
	if item == nil {
		return
	}

	result, err := m.Send(SendRequest{
		Prompt:        item.Text,
		SessionID:     sessionID,
		Agent:         item.Agent,
		AgentSub:      item.AgentSub,
		Model:         item.Model,
		Effort:        item.Effort,
		AttachmentIDs: item.Attachments,
	})
	if err != nil {
		log.Printf("processNextFromQueue Send failed: %v", err)
		m.MarkQueueItemFailed(item.ID, err.Error())
		return
	}

	if result.MessageID > 0 {
		_, _ = m.db.Exec(`UPDATE follow_up_queue SET message_id = ? WHERE id = ?`, result.MessageID, item.ID)
	}

	bc := m.GetBroadcaster()
	for evt := range result.Events {
		switch evt.Type {
		case ChanAction:
			var actionData map[string]interface{}
			if err := json.Unmarshal([]byte(evt.JSON), &actionData); err == nil {
				bc.PublishAction(sessionID, "", actionData)
			}
		case ChanAskUser:
			var askData map[string]interface{}
			if err := json.Unmarshal([]byte(evt.JSON), &askData); err == nil {
				bc.PublishSessionEvent(sessionID, SSEMessage, map[string]interface{}{
					"type": "ask_user", "data": askData,
				})
			}
		case ChanText:
			bc.PublishChunk(sessionID, "", evt.Text)
		}
	}
	bc.PublishDone(sessionID, "")
	m.MarkQueueItemCompleted(item.ID, result.MessageID)
}
