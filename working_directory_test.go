package mux

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	gfs "github.com/apsis-ai/gimbal/filesystem"
)

type fakeFS struct {
	home string
	dirs map[string]gfs.Entry
}

func newTestManagerWithFilesystem(t *testing.T, fs fakeFS) *Manager {
	t.Helper()
	cfg := tempConfig(t)
	cfg.Filesystem = fs
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func tempHome(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func tempDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func fakeFSWithDirs(dirs ...string) fakeFS {
	fs := fakeFS{dirs: make(map[string]gfs.Entry)}
	for _, dir := range dirs {
		fs.dirs[filepath.Clean(dir)] = directoryEntry(filepath.Clean(dir))
	}
	return fs
}

func directoryEntry(path string) gfs.Entry {
	return gfs.Entry{
		Path:       path,
		Name:       filepath.Base(path),
		Type:       gfs.EntryTypeDirectory,
		Readable:   true,
		Searchable: true,
	}
}

func (fs fakeFS) Home(context.Context) (gfs.Entry, error) {
	if fs.home == "" {
		return gfs.Entry{}, errors.New("home missing")
	}
	return directoryEntry(filepath.Clean(fs.home)), nil
}

func (fs fakeFS) Stat(_ context.Context, path string, _ gfs.StatOptions) (gfs.Entry, error) {
	path = filepath.Clean(path)
	if fs.home != "" && path == filepath.Clean(fs.home) {
		return directoryEntry(path), nil
	}
	if entry, ok := fs.dirs[path]; ok {
		return entry, nil
	}
	return gfs.Entry{}, errors.New("missing")
}

func (fs fakeFS) List(context.Context, string, gfs.ListOptions) ([]gfs.Entry, error) {
	return nil, nil
}

func insertSessionWithCWD(t *testing.T, db *sql.DB, id, cwd string, lastActiveAt int64) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO sessions (id, status, handoff_path, conversation_id, token_usage, created_at, last_active_at, working_directory)
		 VALUES (?, ?, '', NULL, NULL, ?, ?, ?)`,
		id, StatusIdle, lastActiveAt-1, lastActiveAt, cwd,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func insertLegacySessionWithoutCWD(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	now := time.Now().Unix()
	_, err := db.Exec(
		`INSERT INTO sessions (id, status, handoff_path, conversation_id, token_usage, created_at, last_active_at)
		 VALUES (?, ?, '', NULL, NULL, ?, ?)`,
		id, StatusIdle, now, now,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func registerFakeProvider(t *testing.T, m *Manager) {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "fake-agent")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho ok\n"), 0755); err != nil {
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
}

func storedWorkingDirectory(t *testing.T, db *sql.DB, sessionID string) string {
	t.Helper()
	var cwd string
	if err := db.QueryRow(`SELECT COALESCE(working_directory, '') FROM sessions WHERE id = ?`, sessionID).Scan(&cwd); err != nil {
		t.Fatal(err)
	}
	return cwd
}

func TestCreateSessionDefaultsWorkingDirectoryToHome(t *testing.T) {
	home := tempHome(t)
	m := newTestManagerWithFilesystem(t, fakeFS{home: home})

	if err := m.CreateSession("cwd-home"); err != nil {
		t.Fatal(err)
	}
	sessions, err := m.ListSessions()
	if err != nil {
		t.Fatal(err)
	}

	if sessions[0].WorkingDirectory != home {
		t.Fatalf("working directory = %q, want %q", sessions[0].WorkingDirectory, home)
	}
}

func TestCreateSessionUsesLatestValidRecentWorkingDirectory(t *testing.T) {
	recent := tempDir(t)
	older := tempDir(t)
	m := newTestManagerWithFilesystem(t, fakeFSWithDirs(recent, older))
	insertSessionWithCWD(t, m.db, "older", older, 10)
	insertSessionWithCWD(t, m.db, "recent", recent, 20)

	if err := m.CreateSession("next"); err != nil {
		t.Fatal(err)
	}
	got := storedWorkingDirectory(t, m.db, "next")

	if got != recent {
		t.Fatalf("working directory = %q, want %q", got, recent)
	}
}

func TestSendBackfillsWorkingDirectoryForLegacySession(t *testing.T) {
	home := tempHome(t)
	m := newTestManagerWithFilesystem(t, fakeFS{home: home})
	registerFakeProvider(t, m)
	insertLegacySessionWithoutCWD(t, m.db, "legacy")

	_, err := m.Send(SendRequest{SessionID: "legacy", Prompt: "pwd", ProviderID: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	got := storedWorkingDirectory(t, m.db, "legacy")

	if got != home {
		t.Fatalf("working directory = %q, want %q", got, home)
	}
}

func TestSetWorkingDirectoryAllowsBusySession(t *testing.T) {
	home := tempHome(t)
	project := tempDir(t)
	fs := fakeFSWithDirs(project)
	fs.home = home
	m := newTestManagerWithFilesystem(t, fs)
	if err := m.CreateSession("busy"); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.activeProcesses["busy"] = &processEntry{Pid: 1234}
	m.mu.Unlock()

	session, err := m.SetWorkingDirectory("busy", project)

	if err != nil {
		t.Fatalf("SetWorkingDirectory returned error: %v", err)
	}
	if session.WorkingDirectory != project {
		t.Fatalf("working directory = %q, want %q", session.WorkingDirectory, project)
	}
}

func TestSetWorkingDirectoryClearsConversationID(t *testing.T) {
	home := tempHome(t)
	project := tempDir(t)
	fs := fakeFSWithDirs(project)
	fs.home = home
	m := newTestManagerWithFilesystem(t, fs)
	if err := m.CreateSession("cwd"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.db.Exec(`UPDATE sessions SET conversation_id = 'abc' WHERE id = 'cwd'`); err != nil {
		t.Fatal(err)
	}

	if _, err := m.SetWorkingDirectory("cwd", project); err != nil {
		t.Fatal(err)
	}

	var conversationID string
	_ = m.db.QueryRow(`SELECT COALESCE(conversation_id, '') FROM sessions WHERE id = 'cwd'`).Scan(&conversationID)
	if conversationID != "" {
		t.Fatalf("conversation_id = %q", conversationID)
	}
}

func TestSetWorkingDirectoryResolvesRelativePathFromCurrentWorkingDirectory(t *testing.T) {
	home := tempHome(t)
	project := filepath.Join(home, "project")
	fs := fakeFSWithDirs(project)
	fs.home = home
	m := newTestManagerWithFilesystem(t, fs)
	if err := m.CreateSession("relative"); err != nil {
		t.Fatal(err)
	}

	session, err := m.SetWorkingDirectory("relative", "project")
	if err != nil {
		t.Fatal(err)
	}

	if session.WorkingDirectory != project {
		t.Fatalf("working directory = %q, want %q", session.WorkingDirectory, project)
	}
}

func TestSendInvalidWorkingDirectoryDoesNotPersistUserMessage(t *testing.T) {
	home := tempHome(t)
	fs := fakeFSWithDirs("/tmp/valid")
	fs.home = home
	m := newTestManagerWithFilesystem(t, fs)
	if err := m.CreateSession("bad-cwd"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.db.Exec(`UPDATE sessions SET working_directory = ? WHERE id = ?`, "/tmp/missing", "bad-cwd"); err != nil {
		t.Fatal(err)
	}

	_, err := m.Send(SendRequest{SessionID: "bad-cwd", Prompt: "pwd", ProviderID: "fake"})
	if err == nil {
		t.Fatal("expected working directory error")
	}
	msgs, _ := m.GetMessages("bad-cwd")
	if len(msgs) != 0 {
		t.Fatalf("messages persisted on cwd failure: %#v", msgs)
	}
}
