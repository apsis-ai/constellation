package mux

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// appendConversation appends a ConversationEntry to the session's conversation.jsonl.
func (m *Manager) appendConversation(sessionID string, entry ConversationEntry) {
	if entry.Timestamp == "" {
		entry.Timestamp = nowRFC3339()
	}
	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("marshal conversation entry: %v", err)
		return
	}
	mu := m.sessionFileMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	path := filepath.Join(m.sessionDir(sessionID), "conversation.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("open conversation file: %v", err)
		return
	}
	defer f.Close()
	f.Write(append(data, '\n'))
}

// readConversation reads all entries from a session's conversation.jsonl.
func (m *Manager) readConversation(sessionID string) ([]ConversationEntry, error) {
	path := filepath.Join(m.config.SessionDir, sessionID, "conversation.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var entries []ConversationEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		entry := ConversationEntry{}
		if v, ok := raw["ts"].(string); ok {
			entry.Timestamp = v
		}
		if v, ok := raw["role"].(string); ok {
			entry.Role = v
		}
		if v, ok := raw["content"]; ok {
			entry.Content = normalizeContent(v)
		}
		if v, ok := raw["agent"].(string); ok {
			entry.Agent = v
		}
		if v, ok := raw["tool"].(string); ok {
			entry.Tool = v
		}
		if v, ok := raw["input"].(map[string]interface{}); ok {
			entry.Input = v
		}
		if v, ok := raw["result"].(string); ok {
			entry.Result = v
		}
		if v, ok := raw["summary"].(string); ok {
			entry.Summary = v
		}
		if v, ok := raw["origin"].(string); ok {
			entry.Origin = v
		}
		if v, ok := raw["display_as"].(string); ok {
			entry.DisplayAs = v
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// normalizeContent handles content as string or []interface{} (Anthropic/OpenAI blocks).
func normalizeContent(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	arr, ok := v.([]interface{})
	if !ok {
		return ""
	}
	var parts []string
	for _, item := range arr {
		block, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if block["type"] != "text" {
			continue
		}
		if text, ok := block["text"].(string); ok {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "")
}

// writeSummary atomically writes the session's summary.json.
func (m *Manager) writeSummary(sessionID string, summary SessionSummary) {
	if summary.UpdatedAt == "" {
		summary.UpdatedAt = nowRFC3339()
	}
	data, err := json.Marshal(summary)
	if err != nil {
		return
	}
	mu := m.sessionFileMu(sessionID)
	mu.Lock()
	defer mu.Unlock()
	dir := m.sessionDir(sessionID)
	tmpPath := filepath.Join(dir, "summary.json.tmp")
	finalPath := filepath.Join(dir, "summary.json")
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return
	}
	os.Rename(tmpPath, finalPath)
}

// readSummary reads a session's summary.json.
func (m *Manager) readSummary(sessionID string) SessionSummary {
	path := filepath.Join(m.config.SessionDir, sessionID, "summary.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return SessionSummary{}
	}
	var s SessionSummary
	json.Unmarshal(data, &s)
	return s
}
