package mux

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AddToQueue inserts a follow-up message into the queue for a session.
func (m *Manager) AddToQueue(req QueueAddRequest) (*QueueItem, error) {
	if req.SessionID == "" {
		return nil, fmt.Errorf("session_id required")
	}
	if req.Text == "" {
		return nil, fmt.Errorf("text required")
	}
	source := req.Source
	if source == "" {
		source = "text"
	}
	messageID := strings.TrimSpace(req.MessageID)
	if messageID == "" {
		messageID = uuid.NewString()
	}
	responseID := strings.TrimSpace(req.ResponseID)
	if responseID == "" {
		responseID = uuid.NewString()
	}

	now := time.Now().Unix()
	_, _ = m.db.Exec(`INSERT OR IGNORE INTO sessions (id, status, handoff_path, conversation_id, token_usage, created_at, last_active_at)
		VALUES (?, ?, '', NULL, NULL, ?, ?)`, req.SessionID, StatusActive, now, now)

	workingDirectory := strings.TrimSpace(req.WorkingDirectory)
	var err error
	if workingDirectory == "" {
		workingDirectory, err = m.ensureSessionWorkingDirectory(context.Background(), m.db, req.SessionID)
		if err != nil {
			return nil, fmt.Errorf("working directory: %w", err)
		}
	} else {
		base, err := m.ensureSessionWorkingDirectory(context.Background(), m.db, req.SessionID)
		if err != nil {
			return nil, fmt.Errorf("working directory: %w", err)
		}
		workingDirectory, err = m.validateWorkingDirectory(context.Background(), workingDirectory, base)
		if err != nil {
			return nil, fmt.Errorf("working directory: %w", err)
		}
	}

	providerID := req.ProviderID
	configValues := req.ConfigValues
	agent := req.Agent
	agentSub := req.AgentSub
	model := req.Model
	effort := req.Effort
	var lastAgent, lastAgentSub, lastModel, lastEffort, lastProviderID, lastConfigValuesJSON string
	_ = m.db.QueryRow(`SELECT COALESCE(last_agent,'claude'), COALESCE(last_agent_sub,''), COALESCE(last_model,''), COALESCE(last_effort,''), COALESCE(provider_id,''), COALESCE(config_values_json,'{}') FROM sessions WHERE id = ?`, req.SessionID).Scan(&lastAgent, &lastAgentSub, &lastModel, &lastEffort, &lastProviderID, &lastConfigValuesJSON)
	if lastAgent == "" {
		lastAgent = "claude"
	}
	if providerID == "" {
		providerID = lastProviderID
	}
	if providerID == "" {
		providerID = agent
	}
	if providerID == "" {
		providerID = lastAgent
	}
	if providerID == "" {
		providerID = "claude"
	}
	if configValues == nil || len(configValues) == 0 {
		configValues = UnmarshalConfigValues(lastConfigValuesJSON)
	}
	if agent == "" {
		agent = providerID
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
	_ = m.db.QueryRow(`SELECT MAX(position) FROM follow_up_queue WHERE session_id = ? AND status = 'pending'`, req.SessionID).Scan(&maxPos)
	position := 1
	if maxPos.Valid {
		position = int(maxPos.Int64) + 1
	}

	id := uuid.New().String()
	attJSON := "[]"
	if len(req.Attachments) > 0 {
		b, _ := json.Marshal(req.Attachments)
		attJSON = string(b)
	}

	_, err = m.db.Exec(`INSERT INTO follow_up_queue (id, session_id, text, position, provider_id, config_values_json, agent, agent_sub, model, effort, attachments, created_at, source, status, transcript, message_id, response_id, working_directory)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?)`,
		id, req.SessionID, req.Text, position, providerID, MarshalConfigValues(configValues), agent, agentSub, model, effort, attJSON, now, source, req.Transcript, messageID, responseID, workingDirectory)
	if err != nil {
		return nil, err
	}

	return &QueueItem{
		ID:               id,
		SessionID:        req.SessionID,
		Text:             req.Text,
		WorkingDirectory: workingDirectory,
		Position:         position,
		ProviderID:       providerID,
		ConfigValues:     configValues,
		Agent:            agent,
		AgentSub:         agentSub,
		Model:            model,
		Effort:           effort,
		Attachments:      req.Attachments,
		CreatedAt:        now,
		Source:           source,
		Status:           "pending",
		Transcript:       req.Transcript,
		MessageID:        messageID,
		ResponseID:       responseID,
	}, nil
}

// ListQueue returns pending queue items for a session ordered by position ascending.
func (m *Manager) ListQueue(sessionID string) ([]QueueItem, error) {
	rows, err := m.db.Query(`SELECT id, session_id, text, COALESCE(working_directory, ''), position, COALESCE(provider_id,''), COALESCE(config_values_json,'{}'), agent, agent_sub, model, effort, COALESCE(attachments,'[]'), created_at, source, status, transcript, CAST(message_id AS TEXT), COALESCE(response_id, ''), started_at, completed_at, error
		FROM follow_up_queue WHERE session_id = ? AND status = 'pending' ORDER BY position ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanQueueRows(rows)
}

// ListQueueAll returns all queue items for a session (including completed/failed) ordered by position ascending.
func (m *Manager) ListQueueAll(sessionID string) ([]QueueItem, error) {
	rows, err := m.db.Query(`SELECT id, session_id, text, COALESCE(working_directory, ''), position, COALESCE(provider_id,''), COALESCE(config_values_json,'{}'), agent, agent_sub, model, effort, COALESCE(attachments,'[]'), created_at, source, status, transcript, CAST(message_id AS TEXT), COALESCE(response_id, ''), started_at, completed_at, error
		FROM follow_up_queue WHERE session_id = ? ORDER BY position ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanQueueRows(rows)
}

func scanQueueRows(rows *sql.Rows) ([]QueueItem, error) {
	var items []QueueItem
	for rows.Next() {
		var q QueueItem
		var attJSON string
		var configValuesJSON string
		if err := rows.Scan(&q.ID, &q.SessionID, &q.Text, &q.WorkingDirectory, &q.Position, &q.ProviderID, &configValuesJSON, &q.Agent, &q.AgentSub, &q.Model, &q.Effort, &attJSON, &q.CreatedAt, &q.Source, &q.Status, &q.Transcript, &q.MessageID, &q.ResponseID, &q.StartedAt, &q.CompletedAt, &q.Error); err != nil {
			return nil, err
		}
		if q.ProviderID == "" {
			q.ProviderID = q.Agent
		}
		q.ConfigValues = UnmarshalConfigValues(configValuesJSON)
		if attJSON != "" && attJSON != "[]" {
			_ = json.Unmarshal([]byte(attJSON), &q.Attachments)
		}
		items = append(items, q)
	}
	return items, nil
}

// UpdateQueueItem updates fields of a pending queue item. Only non-nil fields in
// update are applied. Returns error if item not found or already processed.
func (m *Manager) UpdateQueueItem(sessionID, itemID string, update QueueItemUpdate) (*QueueItem, error) {
	if update.Text == nil && update.WorkingDirectory == nil {
		return nil, fmt.Errorf("queue update must set text or working_directory")
	}

	sets := make([]string, 0, 2)
	args := make([]any, 0, 4)

	if update.Text != nil {
		sets = append(sets, "text = ?")
		args = append(args, *update.Text)
	}
	if update.WorkingDirectory != nil {
		base, err := m.ensureSessionWorkingDirectory(context.Background(), m.db, sessionID)
		if err != nil {
			return nil, fmt.Errorf("working directory: %w", err)
		}
		resolved, err := m.validateWorkingDirectory(context.Background(), *update.WorkingDirectory, base)
		if err != nil {
			return nil, fmt.Errorf("working directory: %w", err)
		}
		sets = append(sets, "working_directory = ?")
		args = append(args, resolved)
	}

	args = append(args, itemID, sessionID)
	query := fmt.Sprintf(`UPDATE follow_up_queue SET %s WHERE id = ? AND session_id = ? AND status = 'pending'`, strings.Join(sets, ", "))
	res, err := m.db.Exec(query, args...)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, fmt.Errorf("queue item %s not found or already processed", itemID)
	}
	var q QueueItem
	var attJSON string
	var configValuesJSON string
	err = m.db.QueryRow(`SELECT id, session_id, text, COALESCE(working_directory, ''), position, COALESCE(provider_id,''), COALESCE(config_values_json,'{}'), agent, agent_sub, model, effort, COALESCE(attachments,'[]'), created_at, source, status, transcript, CAST(message_id AS TEXT), COALESCE(response_id, ''), started_at, completed_at, error
		FROM follow_up_queue WHERE id = ?`, itemID).Scan(&q.ID, &q.SessionID, &q.Text, &q.WorkingDirectory, &q.Position, &q.ProviderID, &configValuesJSON, &q.Agent, &q.AgentSub, &q.Model, &q.Effort, &attJSON, &q.CreatedAt, &q.Source, &q.Status, &q.Transcript, &q.MessageID, &q.ResponseID, &q.StartedAt, &q.CompletedAt, &q.Error)
	if err != nil {
		return nil, err
	}
	q.ConfigValues = UnmarshalConfigValues(configValuesJSON)
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
	var configValuesJSON string
	err = tx.QueryRow(`SELECT id, session_id, text, COALESCE(working_directory, ''), position, COALESCE(provider_id,''), COALESCE(config_values_json,'{}'), agent, agent_sub, model, effort, attachments, created_at, source, transcript, CAST(message_id AS TEXT), COALESCE(response_id, '')
		FROM follow_up_queue WHERE session_id = ? AND status = 'pending' ORDER BY position ASC LIMIT 1`, sessionID).Scan(
		&q.ID, &q.SessionID, &q.Text, &q.WorkingDirectory, &q.Position, &q.ProviderID, &configValuesJSON, &q.Agent, &q.AgentSub, &q.Model, &q.Effort, &attJSON, &q.CreatedAt, &q.Source, &q.Transcript, &q.MessageID, &q.ResponseID)
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
	if q.ProviderID == "" {
		q.ProviderID = q.Agent
	}
	q.ConfigValues = UnmarshalConfigValues(configValuesJSON)
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
func (m *Manager) MarkQueueItemCompleted(itemID string, messageID string) {
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

	providerID := item.ProviderID
	if providerID == "" {
		providerID = item.Agent
	}
	result, err := m.Send(SendRequest{
		Prompt:           item.Text,
		SessionID:        sessionID,
		ProviderID:       providerID,
		ConfigValues:     item.ConfigValues,
		Agent:            providerID,
		AgentSub:         item.AgentSub,
		Model:            item.Model,
		Effort:           item.Effort,
		AttachmentIDs:    item.Attachments,
		MessageID:        item.MessageID,
		ResponseID:       item.ResponseID,
		WorkingDirectory: item.WorkingDirectory,
	})
	if err != nil {
		log.Printf("processNextFromQueue Send failed: %v", err)
		m.MarkQueueItemFailed(item.ID, err.Error())
		return
	}

	if result.UserMessageID != "" {
		_, _ = m.db.Exec(`UPDATE follow_up_queue SET message_id = ?, response_id = ? WHERE id = ?`, result.UserMessageID, result.ResponseMessageID, item.ID)
	}

	bc := m.GetBroadcaster()
	for evt := range result.Events {
		switch evt.Type {
		case ChanAction:
			var actionData map[string]interface{}
			if err := json.Unmarshal([]byte(evt.JSON), &actionData); err == nil {
				bc.PublishAction(sessionID, result.ResponseMessageID, actionData)
			}
		case ChanAskUser:
			var askData map[string]interface{}
			if err := json.Unmarshal([]byte(evt.JSON), &askData); err == nil {
				bc.PublishSessionEvent(sessionID, SSEMessage, map[string]interface{}{
					"type": "ask_user", "data": askData,
				})
			}
		case ChanText:
			bc.PublishChunk(sessionID, result.ResponseMessageID, evt.Text)
		}
	}
	bc.PublishDone(sessionID, result.ResponseMessageID)
	m.MarkQueueItemCompleted(item.ID, result.UserMessageID)
}
