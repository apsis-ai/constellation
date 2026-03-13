package mux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultEnvProvider_CreatesIsolatedDirs(t *testing.T) {
	tmpDir := t.TempDir()
	p := &DefaultEnvProvider{Base: tmpDir}
	env := p.AgentEnv()

	// Check that isolated directories were created
	claudeDir := filepath.Join(tmpDir, "claude")
	codexDir := filepath.Join(tmpDir, "codex")
	opencodeDir := filepath.Join(tmpDir, "opencode")

	for _, dir := range []string{claudeDir, codexDir, opencodeDir} {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("expected directory to be created: %s", dir)
		}
	}

	// Check env vars are set
	envMap := envToMap(env)
	if v, ok := envMap["CODEX_HOME"]; !ok || v != codexDir {
		t.Errorf("expected CODEX_HOME=%s, got %q", codexDir, v)
	}
	if v, ok := envMap["OPENCODE_CONFIG_DIR"]; !ok || v != opencodeDir {
		t.Errorf("expected OPENCODE_CONFIG_DIR=%s, got %q", opencodeDir, v)
	}
}

func TestDefaultEnvProvider_StripsClaudeCode(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CLAUDECODE", "something")
	p := &DefaultEnvProvider{Base: tmpDir}
	env := p.AgentEnv()

	for _, e := range env {
		if strings.HasPrefix(e, "CLAUDECODE=") {
			t.Error("expected CLAUDECODE to be stripped from env")
		}
	}
}

func TestDefaultEnvProvider_OnceSetup(t *testing.T) {
	tmpDir := t.TempDir()
	p := &DefaultEnvProvider{Base: tmpDir}

	// Call twice - should be idempotent
	env1 := p.AgentEnv()
	env2 := p.AgentEnv()

	if len(env1) == 0 || len(env2) == 0 {
		t.Error("expected non-empty env slices")
	}
}

func TestEnvWithout(t *testing.T) {
	env := []string{"FOO=bar", "BAZ=qux", "FOO_OTHER=keep"}
	result := envWithout(env, "FOO")

	for _, e := range result {
		if e == "FOO=bar" {
			t.Error("expected FOO=bar to be removed")
		}
	}
	// FOO_OTHER should remain (prefix matching should be exact)
	found := false
	for _, e := range result {
		if e == "FOO_OTHER=keep" {
			found = true
		}
	}
	if !found {
		t.Error("expected FOO_OTHER=keep to remain")
	}
}

func TestEnvSet_New(t *testing.T) {
	env := []string{"A=1"}
	result := envSet(env, "B", "2")
	m := envToMap(result)
	if m["B"] != "2" {
		t.Errorf("expected B=2, got %q", m["B"])
	}
	if m["A"] != "1" {
		t.Errorf("expected A=1 to remain, got %q", m["A"])
	}
}

func TestEnvSet_Replace(t *testing.T) {
	env := []string{"A=1", "B=old"}
	result := envSet(env, "B", "new")
	m := envToMap(result)
	if m["B"] != "new" {
		t.Errorf("expected B=new, got %q", m["B"])
	}
}

func TestSymlinkIfMissing(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source file
	src := filepath.Join(tmpDir, "source")
	os.WriteFile(src, []byte("content"), 0644)

	// Create symlink
	dst := filepath.Join(tmpDir, "link")
	symlinkIfMissing(src, dst)

	// Verify symlink exists
	info, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("expected symlink to be created: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("expected a symlink")
	}

	// Call again - should be idempotent (no error)
	symlinkIfMissing(src, dst)
}

// envToMap converts an env slice to a map for easier testing.
func envToMap(env []string) map[string]string {
	m := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}
