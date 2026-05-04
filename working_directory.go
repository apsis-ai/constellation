package mux

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	gfs "github.com/apsis-ai/gimbal/filesystem"
)

var (
	ErrWorkingDirectoryEmpty           = errors.New("empty_path")
	ErrWorkingDirectoryNotFound        = errors.New("not_found")
	ErrWorkingDirectoryNotDirectory    = errors.New("not_directory")
	ErrWorkingDirectoryNotReadable     = errors.New("not_readable")
	ErrWorkingDirectorySessionBusy     = errors.New("session_busy")
	ErrWorkingDirectoryHomeUnavailable = errors.New("home_unavailable")
	ErrWorkingDirectorySessionNotFound = errors.New("session_not_found")
)

type sqlExec interface {
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

func (m *Manager) filesystem() gfs.Provider {
	if m.config.Filesystem != nil {
		return m.config.Filesystem
	}
	return gfs.NewHostProvider()
}

func (m *Manager) validateWorkingDirectory(ctx context.Context, input, base string) (string, error) {
	resolved, err := m.resolveWorkingDirectoryInput(input, base)
	if err != nil {
		return "", err
	}
	entry, err := m.filesystem().Stat(ctx, resolved, gfs.StatOptions{FollowSymlinks: true})
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrWorkingDirectoryNotFound, err)
	}
	if entry.Type != gfs.EntryTypeDirectory {
		return "", ErrWorkingDirectoryNotDirectory
	}
	if !entry.Readable || !entry.Searchable {
		return "", ErrWorkingDirectoryNotReadable
	}
	return entry.Path, nil
}

func (m *Manager) resolveWorkingDirectoryInput(input, base string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", ErrWorkingDirectoryEmpty
	}
	if input == "~" || strings.HasPrefix(input, "~/") {
		home, err := m.filesystem().Home(context.Background())
		if err != nil || home.Path == "" {
			return "", ErrWorkingDirectoryHomeUnavailable
		}
		if input == "~" {
			return home.Path, nil
		}
		return filepath.Join(home.Path, input[2:]), nil
	}
	if filepath.IsAbs(input) {
		return filepath.Clean(input), nil
	}
	if base == "" {
		home, err := m.filesystem().Home(context.Background())
		if err != nil || home.Path == "" {
			return "", ErrWorkingDirectoryHomeUnavailable
		}
		base = home.Path
	}
	return filepath.Clean(filepath.Join(base, input)), nil
}

func (m *Manager) defaultWorkingDirectory(ctx context.Context) (string, error) {
	if strings.TrimSpace(m.config.WorkingDirectoryDefault) != "" {
		if cwd, err := m.validateWorkingDirectory(ctx, m.config.WorkingDirectoryDefault, ""); err == nil {
			return cwd, nil
		}
	}

	rows, err := m.db.Query(`SELECT working_directory FROM sessions WHERE COALESCE(working_directory, '') != '' ORDER BY last_active_at DESC`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var cwd string
		if err := rows.Scan(&cwd); err != nil {
			return "", err
		}
		if validated, err := m.validateWorkingDirectory(ctx, cwd, ""); err == nil {
			return validated, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	home, err := m.filesystem().Home(ctx)
	if err != nil || home.Path == "" {
		return "", ErrWorkingDirectoryHomeUnavailable
	}
	return m.validateWorkingDirectory(ctx, home.Path, "")
}

func (m *Manager) ensureSessionWorkingDirectory(ctx context.Context, exec sqlExec, sessionID string) (string, error) {
	var current string
	err := exec.QueryRow(`SELECT COALESCE(working_directory, '') FROM sessions WHERE id = ?`, sessionID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrWorkingDirectorySessionNotFound
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(current) != "" {
		return m.validateWorkingDirectory(ctx, current, "")
	}
	cwd, err := m.defaultWorkingDirectory(ctx)
	if err != nil {
		return "", err
	}
	if _, err := exec.Exec(`UPDATE sessions SET working_directory = ? WHERE id = ?`, cwd, sessionID); err != nil {
		return "", err
	}
	return cwd, nil
}

func (m *Manager) GetWorkingDirectory(sessionID string) (string, error) {
	var cwd string
	err := m.db.QueryRow(`SELECT COALESCE(working_directory, '') FROM sessions WHERE id = ?`, sessionID).Scan(&cwd)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrWorkingDirectorySessionNotFound
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cwd) == "" {
		return m.ensureSessionWorkingDirectory(context.Background(), m.db, sessionID)
	}
	return m.validateWorkingDirectory(context.Background(), cwd, "")
}

func (m *Manager) SetWorkingDirectory(sessionID, path string) (Session, error) {
	var status SessionStatus
	var current string
	err := m.db.QueryRow(`SELECT status, COALESCE(working_directory, '') FROM sessions WHERE id = ?`, sessionID).Scan(&status, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrWorkingDirectorySessionNotFound
	}
	if err != nil {
		return Session{}, err
	}
	if m.isSessionBusy(sessionID, status) {
		return Session{}, ErrWorkingDirectorySessionBusy
	}
	canonical, err := m.validateWorkingDirectory(context.Background(), path, current)
	if err != nil {
		return Session{}, err
	}
	_, err = m.db.Exec(
		`UPDATE sessions SET working_directory = ?, conversation_id = NULL, last_active_at = ? WHERE id = ?`,
		canonical, nowUnix(), sessionID,
	)
	if err != nil {
		return Session{}, err
	}
	return m.getSession(sessionID)
}

func (m *Manager) isSessionBusy(sessionID string, status SessionStatus) bool {
	if status == StatusActive || status == StatusWaiting {
		return true
	}
	m.mu.Lock()
	_, active := m.activeProcesses[sessionID]
	m.mu.Unlock()
	return active
}
