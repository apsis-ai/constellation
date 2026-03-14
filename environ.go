package mux

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// DefaultEnvProvider creates isolated config directories for spawned agents.
// It strips CLAUDECODE env vars and sets CODEX_HOME, OPENCODE_CONFIG_DIR
// to isolated directories so agents don't inherit host config.
type DefaultEnvProvider struct {
	once sync.Once
	// Base is the root directory for isolated config dirs.
	// If empty, defaults to os.TempDir()/constellation-config.
	Base string
	// SymlinkPaths maps config items to symlink. Optional customization.
	// Keys are destination relative paths, values are source absolute paths.
	SymlinkPaths map[string]string
}

// AgentEnv returns environment variables for spawned agent processes with isolated config.
func (p *DefaultEnvProvider) AgentEnv() []string {
	p.once.Do(p.setup)

	env := envWithout(os.Environ(), "CLAUDECODE")
	env = envSet(env, "CODEX_HOME", filepath.Join(p.base(), "codex"))
	env = envSet(env, "OPENCODE_CONFIG_DIR", filepath.Join(p.base(), "opencode"))
	return env
}

func (p *DefaultEnvProvider) base() string {
	if p.Base != "" {
		return p.Base
	}
	return filepath.Join(os.TempDir(), "constellation-config")
}

// defaultAuthSymlinks returns symlinks needed to propagate CLI auth into isolated dirs.
// Each entry maps a relative destination (under base) to an absolute source path.
func defaultAuthSymlinks() map[string]string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}
	links := make(map[string]string)

	// Codex: auth.json holds OAuth/API tokens
	codexAuth := filepath.Join(home, ".codex", "auth.json")
	if _, err := os.Stat(codexAuth); err == nil {
		links["codex/auth.json"] = codexAuth
	}

	// Codex: config.toml holds model preferences and settings
	codexConfig := filepath.Join(home, ".codex", "config.toml")
	if _, err := os.Stat(codexConfig); err == nil {
		links["codex/config.toml"] = codexConfig
	}

	// OpenCode: opencode.json holds provider config and model settings
	opencodeConfig := filepath.Join(home, ".config", "opencode", "opencode.json")
	if _, err := os.Stat(opencodeConfig); err == nil {
		links["opencode/opencode.json"] = opencodeConfig
	}

	return links
}

func (p *DefaultEnvProvider) setup() {
	base := p.base()

	// Create isolated directories for each agent type
	claudeDir := filepath.Join(base, "claude")
	codexDir := filepath.Join(base, "codex")
	opencodeDir := filepath.Join(base, "opencode")

	os.MkdirAll(claudeDir, 0755)
	os.MkdirAll(codexDir, 0755)
	os.MkdirAll(opencodeDir, 0755)

	// Symlink auth files from real config dirs so agents inherit system auth
	for dst, src := range defaultAuthSymlinks() {
		dstPath := filepath.Join(base, dst)
		os.MkdirAll(filepath.Dir(dstPath), 0755)
		symlinkIfMissing(src, dstPath)
	}

	// Apply custom symlinks (override defaults if overlapping)
	for dst, src := range p.SymlinkPaths {
		dstPath := filepath.Join(base, dst)
		os.MkdirAll(filepath.Dir(dstPath), 0755)
		symlinkIfMissing(src, dstPath)
	}
}

// symlinkIfMissing creates a symlink at dst pointing to src, skipping if dst exists.
func symlinkIfMissing(src, dst string) {
	if _, err := os.Lstat(dst); err == nil {
		return // already exists
	}
	_ = os.Symlink(src, dst)
}

// envWithout removes all entries with the given key prefix from env.
func envWithout(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}

// envSet sets or replaces a variable in an env slice.
func envSet(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
