package mux

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// Config holds Manager configuration.
type Config struct {
	// DBPath is the SQLite database file path. Required.
	DBPath string
	// SessionDir is the base directory for per-session files (conversation.jsonl, attachments).
	SessionDir string
	// HandoffDir is the directory for handoff markdown files.
	HandoffDir string
	// RingBufferSize is the per-session SSE ring buffer capacity (default 1024).
	RingBufferSize int
	// MCPProvider generates MCP config for agent subprocesses. Optional.
	MCPProvider MCPConfigProvider
	// ToolExec handles tool calls from HTTP-based agents. Optional.
	ToolExec ToolExecutor
	// ActionFmt formats tool calls into summaries. Optional (uses default).
	ActionFmt ActionSummaryFormatter
	// TitleGen generates session titles. Optional (uses agent CLI fallback).
	TitleGen TitleGenerator
	// SummaryGen generates session summaries. Optional (uses agent CLI fallback).
	SummaryGen SummaryGenerator
	// HandoffHdl persists handoff state. Optional (uses file-based default).
	HandoffHdl HandoffHandler
	// Transcriber for speech-to-text. Optional.
	Transcriber Transcriber
	// IdleTimeout duration before auto-handoff. Default 10 minutes.
	IdleTimeout time.Duration
	// AgentEnv returns environment variables for spawned agent processes. Optional.
	AgentEnv func() []string
}

// Manager manages AI agent sessions.
type Manager struct {
	db              *sql.DB
	config          Config
	idleMap         map[string]*idleEntry
	activeProcesses map[string]*processEntry
	stoppedSessions map[string]bool
	queuePaused     map[string]bool
	askUserPending  map[string]AskUserPending
	lastActions     map[string]*ActionStatus
	lastUserMessage map[string]string
	broadcast       *SessionBroadcaster
	providers       *ProviderRegistry
	mu              sync.Mutex
	fileMu          sync.Map
	summaryTimers   sync.Map
}

type idleEntry struct {
	timer *time.Timer
	mu    sync.Mutex
}

type processEntry struct {
	Pid     int
	Kill    func() error
	AgentID string
}

// NewManager creates a new Manager with the given config.
func NewManager(cfg Config) (*Manager, error) {
	if cfg.DBPath == "" {
		return nil, fmt.Errorf("DBPath is required")
	}
	if cfg.SessionDir == "" {
		cfg.SessionDir = filepath.Join(filepath.Dir(cfg.DBPath), "sessions")
	}
	if cfg.HandoffDir == "" {
		cfg.HandoffDir = filepath.Join(filepath.Dir(cfg.DBPath), "handoffs")
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 10 * time.Minute
	}
	if cfg.ActionFmt == nil {
		cfg.ActionFmt = DefaultActionFormatter{}
	}
	if cfg.RingBufferSize <= 0 {
		cfg.RingBufferSize = 1024
	}

	// Ensure directories exist
	os.MkdirAll(filepath.Dir(cfg.DBPath), 0755)
	os.MkdirAll(cfg.SessionDir, 0755)
	os.MkdirAll(cfg.HandoffDir, 0755)

	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := initDB(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("init database: %w", err)
	}

	parsers := NewParserRegistry()
	provReg, err := NewProviderRegistry(db, parsers)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init provider registry: %w", err)
	}
	if err := provReg.RegisterBuiltins(); err != nil {
		db.Close()
		return nil, fmt.Errorf("register builtins: %w", err)
	}

	m := &Manager{
		db:              db,
		config:          cfg,
		idleMap:         make(map[string]*idleEntry),
		activeProcesses: make(map[string]*processEntry),
		stoppedSessions: make(map[string]bool),
		queuePaused:     make(map[string]bool),
		askUserPending:  make(map[string]AskUserPending),
		lastActions:     make(map[string]*ActionStatus),
		lastUserMessage: make(map[string]string),
		broadcast:       NewSessionBroadcaster(cfg.RingBufferSize),
		providers:       provReg,
	}
	return m, nil
}

// Close closes the database connection.
func (m *Manager) Close() error {
	return m.db.Close()
}

// GetBroadcaster returns the session event broadcaster.
func (m *Manager) GetBroadcaster() *SessionBroadcaster {
	return m.broadcast
}

// GetProviders returns the provider registry.
func (m *Manager) GetProviders() *ProviderRegistry {
	return m.providers
}

// CreateSession creates an empty session row.
func (m *Manager) CreateSession(sessionID string) error {
	if sessionID == "" {
		sessionID = uuid.New().String()
	}
	now := nowUnix()
	_, err := m.db.Exec(
		`INSERT OR IGNORE INTO sessions (id, status, handoff_path, conversation_id, token_usage, created_at, last_active_at)
		VALUES (?, ?, '', NULL, NULL, ?, ?)`,
		sessionID, StatusIdle, now, now,
	)
	return err
}

// ListSessions returns all sessions ordered by last_active_at DESC.
func (m *Manager) ListSessions() ([]Session, error) {
	rows, err := m.db.Query(`SELECT id, status, COALESCE(handoff_path,''), COALESCE(title,''), COALESCE(last_agent,''), COALESCE(last_agent_sub,''), COALESCE(last_model,''), COALESCE(last_effort,''), created_at, last_active_at FROM sessions ORDER BY last_active_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.Status, &s.HandoffPath, &s.Title, &s.LastAgent, &s.LastAgentSub, &s.LastModel, &s.LastEffort, &s.CreatedAt, &s.LastActiveAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

// DeleteSession deletes a session and its files.
func (m *Manager) DeleteSession(sessionID string) error {
	_, err := m.db.Exec(`DELETE FROM sessions WHERE id = ?`, sessionID)
	if err != nil {
		return err
	}
	_, _ = m.db.Exec(`DELETE FROM messages WHERE session_id = ?`, sessionID)
	_, _ = m.db.Exec(`DELETE FROM follow_up_queue WHERE session_id = ?`, sessionID)
	dir := filepath.Join(m.config.SessionDir, sessionID)
	os.RemoveAll(dir)
	m.broadcast.PublishSessionDeleted(sessionID)
	return nil
}

// GetMessages returns messages for a session.
func (m *Manager) GetMessages(sessionID string) ([]Message, error) {
	entries, err := m.readConversation(sessionID)
	if err != nil || len(entries) == 0 {
		return m.getMessagesFromDB(sessionID)
	}
	var messages []Message
	for _, e := range entries {
		if e.Role == "user" || e.Role == "assistant" {
			messages = append(messages, Message{Role: e.Role, Content: e.Content})
		}
	}
	return messages, nil
}

func (m *Manager) getMessagesFromDB(sessionID string) ([]Message, error) {
	rows, err := m.db.Query(`SELECT role, content FROM messages WHERE session_id = ? ORDER BY id ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.Role, &msg.Content); err != nil {
			return nil, err
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}

// GetConversation returns all conversation entries for a session.
func (m *Manager) GetConversation(sessionID string) ([]ConversationEntry, error) {
	return m.readConversation(sessionID)
}

// GetSummary returns the session's summary.
func (m *Manager) GetSummary(sessionID string) SessionSummary {
	return m.readSummary(sessionID)
}

// sessionDir returns the directory for a session's files.
func (m *Manager) sessionDir(sessionID string) string {
	dir := filepath.Join(m.config.SessionDir, sessionID)
	os.MkdirAll(dir, 0755)
	return dir
}

// sessionFileMu returns a per-session mutex for file writes.
func (m *Manager) sessionFileMu(sessionID string) *sync.Mutex {
	v, _ := m.fileMu.LoadOrStore(sessionID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// QueueLength returns the number of pending items in the queue for a session.
func (m *Manager) QueueLength(sessionID string) int {
	if m.db == nil {
		return 0
	}
	var n int
	_ = m.db.QueryRow(`SELECT COUNT(*) FROM follow_up_queue WHERE session_id = ? AND status = 'pending'`, sessionID).Scan(&n)
	return n
}

func initDB(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS sessions (
		id               TEXT PRIMARY KEY,
		status           TEXT NOT NULL DEFAULT 'active',
		handoff_path     TEXT,
		conversation_id  TEXT,
		token_usage      INTEGER,
		created_at       INTEGER NOT NULL,
		last_active_at   INTEGER NOT NULL
	)`)
	if err != nil {
		return err
	}
	// Migrations
	_, _ = db.Exec(`ALTER TABLE sessions ADD COLUMN conversation_id TEXT`)
	_, _ = db.Exec(`ALTER TABLE sessions ADD COLUMN token_usage INTEGER`)
	_, _ = db.Exec(`ALTER TABLE sessions ADD COLUMN screenshot_count INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE sessions ADD COLUMN last_agent TEXT`)
	_, _ = db.Exec(`ALTER TABLE sessions ADD COLUMN title TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE sessions ADD COLUMN last_agent_sub TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE sessions ADD COLUMN last_model TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE sessions ADD COLUMN last_effort TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE sessions ADD COLUMN pid INTEGER NOT NULL DEFAULT 0`)

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS messages (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		role       TEXT NOT NULL,
		content    TEXT NOT NULL,
		created_at INTEGER NOT NULL
	)`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS follow_up_queue (
		id          TEXT PRIMARY KEY,
		session_id  TEXT NOT NULL,
		text        TEXT NOT NULL,
		position    INTEGER NOT NULL,
		agent       TEXT NOT NULL DEFAULT 'claude',
		agent_sub   TEXT NOT NULL DEFAULT '',
		model       TEXT NOT NULL DEFAULT '',
		effort      TEXT NOT NULL DEFAULT '',
		attachments TEXT,
		created_at  INTEGER NOT NULL
	)`)
	if err != nil {
		return err
	}
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_follow_up_queue_session ON follow_up_queue(session_id)`)
	_, _ = db.Exec(`ALTER TABLE follow_up_queue ADD COLUMN source TEXT NOT NULL DEFAULT 'text'`)
	_, _ = db.Exec(`ALTER TABLE follow_up_queue ADD COLUMN status TEXT NOT NULL DEFAULT 'pending'`)
	_, _ = db.Exec(`ALTER TABLE follow_up_queue ADD COLUMN transcript TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE follow_up_queue ADD COLUMN message_id INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE follow_up_queue ADD COLUMN started_at INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE follow_up_queue ADD COLUMN completed_at INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`ALTER TABLE follow_up_queue ADD COLUMN error TEXT NOT NULL DEFAULT ''`)

	return nil
}
