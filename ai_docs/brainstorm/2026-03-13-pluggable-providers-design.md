# Pluggable Providers Design

**Date:** 2026-03-13
**Status:** Approved for implementation (CLI providers only; HTTP deferred)

## Overview

Refactor the provider/agent system to be dynamically configurable and pluggable:
- **Provider interface** — Abstracts CLI and HTTP-based providers
- **OutputParser interface** — Pluggable output stream parsers
- **ProviderRegistry** — Runtime registration with SQLite persistence
- **Dynamic model discovery** — Providers expose available models at runtime

## Goals

1. Add new providers without modifying core code
2. Persist provider configs in DB (survive restarts)
3. Runtime enable/disable providers
4. Support both CLI-based and HTTP-based providers (HTTP deferred)

## Non-Goals (This Phase)

- HTTP/API-based providers (deferred to future phase)
- Config-driven parsing rules (stick with Go implementations)

---

## Core Interfaces

### Provider

```go
// Provider executes prompts and streams results.
type Provider interface {
    // ID returns the unique provider identifier (e.g., "claude", "my-agent").
    ID() string

    // Execute runs a prompt and streams events to the channel.
    Execute(ctx context.Context, req ProviderRequest, ch chan<- ChanEvent) (ProviderResult, error)

    // SupportsResume returns true if the provider can resume conversations.
    SupportsResume() bool

    // Validate checks if the provider is available/configured correctly.
    Validate() error

    // ListModels returns available models (fetched dynamically).
    ListModels(ctx context.Context) ([]ModelInfo, error)

    // DefaultModel returns the default model ID.
    DefaultModel() string
}

// ProviderRequest contains everything needed to execute a prompt.
type ProviderRequest struct {
    SessionID      string
    Prompt         string
    Model          string
    Effort         string
    SubAgent       string
    ConversationID string // for resume
    Attachments    []AttachmentRef
    MCPConfig      string // for CLI providers that support MCP
    Env            []string
}

// ProviderResult is returned after execution completes.
type ProviderResult struct {
    FullText       string
    ConversationID string
    TokenUsage     int
    UsagePct       float64
}

// ModelInfo describes an available model.
type ModelInfo struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    Description string `json:"description,omitempty"`
    ContextSize int    `json:"context_size,omitempty"`
}
```

### OutputParser

```go
// OutputParser parses streaming output from a provider.
type OutputParser interface {
    // Parse reads from the stream and sends events to the channel.
    Parse(ctx context.Context, sessionID string, r io.Reader, ch chan<- ChanEvent) ProviderResult
}

// Built-in parser types
const (
    ParserTypeClaude   = "claude"   // stream-json format
    ParserTypeCodex    = "codex"    // item.completed/turn.completed
    ParserTypeOpenCode = "opencode" // text/tool_use/step_finish
    ParserTypeCursor   = "cursor"   // claude-like with MCP extensions
    ParserTypeGeneric  = "generic"  // simple text passthrough
)
```

### ParserRegistry

```go
// ParserRegistry holds registered parsers by name.
type ParserRegistry struct {
    parsers map[string]func(m *Manager) OutputParser
}

// RegisterParser adds a custom parser factory.
func (r *ParserRegistry) RegisterParser(name string, factory func(m *Manager) OutputParser)

// GetParser returns a parser by name.
func (r *ParserRegistry) GetParser(name string) (OutputParser, bool)
```

---

## Database Schema

```sql
CREATE TABLE providers (
    id TEXT PRIMARY KEY,           -- e.g., "claude", "my-custom-agent"
    name TEXT NOT NULL,            -- display name
    type TEXT NOT NULL,            -- "cli" or "http"
    parser_type TEXT,              -- "claude", "codex", "generic", etc.
    enabled INTEGER DEFAULT 1,
    priority INTEGER DEFAULT 0,    -- for ordering in UI
    config TEXT NOT NULL,          -- JSON with type-specific fields
    created_at INTEGER,
    updated_at INTEGER
);
```

### CLI Provider Config (JSON)

```json
{
  "binary": "claude",
  "args_template": ["-p", "--output-format", "stream-json", "{{prompt}}"],
  "supports_resume": true,
  "resume_flag": "--resume",
  "model_flag": "--model",
  "effort_flag": "--effort",
  "subagent_flag": "--agent",
  "env_vars": {"CLAUDE_CODE_ISOLATED": "1"}
}
```

### HTTP Provider Config (Future)

```json
{
  "endpoint": "https://api.anthropic.com/v1/messages",
  "auth_type": "bearer",
  "auth_env_var": "ANTHROPIC_API_KEY",
  "headers": {"anthropic-version": "2023-06-01"}
}
```

---

## ProviderRegistry

```go
type ProviderRegistry struct {
    mu        sync.RWMutex
    providers map[string]Provider    // in-memory cache
    parsers   *ParserRegistry
    db        *sql.DB
}

// NewProviderRegistry creates a registry and loads from DB.
func NewProviderRegistry(db *sql.DB) (*ProviderRegistry, error)

// --- Read operations ---

// Get returns a provider by ID.
func (r *ProviderRegistry) Get(id string) (Provider, bool)

// List returns all enabled providers.
func (r *ProviderRegistry) List() []Provider

// ListAll returns all providers including disabled.
func (r *ProviderRegistry) ListAll() []ProviderConfig

// --- Write operations (persist to DB + update cache) ---

// Register adds or updates a provider config in DB and cache.
func (r *ProviderRegistry) Register(cfg ProviderConfig) error

// Unregister removes a provider.
func (r *ProviderRegistry) Unregister(id string) error

// SetEnabled enables/disables a provider.
func (r *ProviderRegistry) SetEnabled(id string, enabled bool) error

// --- Initialization ---

// RegisterBuiltins seeds DB with default providers if not present.
func (r *ProviderRegistry) RegisterBuiltins() error
```

---

## CLIProvider Implementation

```go
// CLIProvider executes prompts via subprocess.
type CLIProvider struct {
    config     CLIProviderConfig
    parser     OutputParser
    manager    *Manager
}

type CLIProviderConfig struct {
    ID             string            `json:"id"`
    Name           string            `json:"name"`
    Binary         string            `json:"binary"`
    ArgsTemplate   []string          `json:"args_template"`
    ParserType     string            `json:"parser_type"`
    SupportsResume bool              `json:"supports_resume"`
    ResumeFlag     string            `json:"resume_flag,omitempty"`
    ModelFlag      string            `json:"model_flag,omitempty"`
    EffortFlag     string            `json:"effort_flag,omitempty"`
    SubAgentFlag   string            `json:"subagent_flag,omitempty"`
    EnvVars        map[string]string `json:"env_vars,omitempty"`
}

func (p *CLIProvider) ID() string { return p.config.ID }

func (p *CLIProvider) Execute(ctx context.Context, req ProviderRequest, ch chan<- ChanEvent) (ProviderResult, error) {
    // 1. Resolve binary path via exec.LookPath
    // 2. Build args from template + request fields
    // 3. Set up process group (Setpgid: true)
    // 4. Merge env vars
    // 5. Create stdout pipe, start process
    // 6. Stream output through parser
    // 7. Wait and return result
}

func (p *CLIProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
    // Try running `{binary} --list-models` or provider-specific command
    // Parse output, fall back to empty list if not supported
}

func (p *CLIProvider) DefaultModel() string {
    // Return from config or discover dynamically
}

func (p *CLIProvider) SupportsResume() bool {
    return p.config.SupportsResume
}

func (p *CLIProvider) Validate() error {
    _, err := exec.LookPath(p.config.Binary)
    return err
}
```

---

## Manager.Send() Integration

```go
func (m *Manager) Send(req SendRequest) (*SendResult, error) {
    // ... session setup (unchanged) ...

    // Get provider from registry instead of switch
    provider, ok := m.providers.Get(req.Agent)
    if !ok {
        return nil, fmt.Errorf("unknown provider: %s", req.Agent)
    }

    if err := provider.Validate(); err != nil {
        return nil, fmt.Errorf("provider %s not available: %w", req.Agent, err)
    }

    ch := make(chan ChanEvent, 32)

    provReq := ProviderRequest{
        SessionID:      sessionID,
        Prompt:         prompt,
        Model:          req.Model,
        Effort:         req.Effort,
        SubAgent:       req.AgentSub,
        ConversationID: conversationID,
        Attachments:    attachments,
    }

    // Add MCP config for providers that support it
    if m.config.MCPProvider != nil {
        provReq.MCPConfig, _ = m.config.MCPProvider.MCPConfig(sessionID, req.Agent)
    }

    go m.runProviderLoop(sessionID, provider, provReq, ch)

    return &SendResult{Events: ch, SessionID: sessionID, MessageID: userMsgID}, nil
}
```

---

## File Structure

```
/provider/
  provider.go        # Provider interface, ProviderRequest, ProviderResult, ModelInfo
  registry.go        # ProviderRegistry with DB persistence
  parser.go          # OutputParser interface, ParserRegistry
  cli_provider.go    # CLIProvider implementation

  # Built-in parsers (refactored from existing parser.go)
  parser_claude.go
  parser_codex.go
  parser_opencode.go
  parser_cursor.go
  parser_generic.go

  # Future
  # http_provider.go  # HTTPProvider for API-based providers
```

---

## Migration Path

1. **Create `provider/` package** with interfaces
2. **Move existing parsers** into `parser_*.go` files implementing `OutputParser`
3. **Create `CLIProvider`** wrapping existing `buildXxxCommand` logic
4. **Create `ProviderRegistry`** with DB persistence
5. **Update `Manager`** to use registry instead of switch statement
6. **Seed DB** with built-in providers on first run
7. **Add management endpoints** (Register/Unregister/List) to HTTP API

---

## Built-in Provider Seeds

On first run, seed these providers into the DB:

| ID | Name | Binary | Parser | Resume |
|----|------|--------|--------|--------|
| claude | Claude | `claude` | claude | yes |
| codex | Codex | `codex` | codex | no |
| opencode | OpenCode | `opencode` | opencode | yes |
| cursor | Cursor | `agent` | cursor | yes |

---

## Future: HTTP Provider (Deferred)

```go
// HTTPProvider calls an API endpoint directly.
type HTTPProvider struct {
    config HTTPProviderConfig
    client *http.Client
}

type HTTPProviderConfig struct {
    ID         string            `json:"id"`
    Name       string            `json:"name"`
    Endpoint   string            `json:"endpoint"`
    AuthType   string            `json:"auth_type"` // "bearer", "api-key", "none"
    AuthEnvVar string            `json:"auth_env_var"`
    Headers    map[string]string `json:"headers,omitempty"`
}
```

This allows calling OpenAI, Anthropic, or other APIs directly without CLI tools.
