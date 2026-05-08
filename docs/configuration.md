# Configuration

## Manager Config

`Config` is passed to `NewManager()` to initialize the library.

Constellation's Go module path is `github.com/apsis-ai/constellation`. The compatibility CLI binary name remains `agents-mux`.

```go
cfg := mux.Config{
    // Required: SQLite database file path.
    DBPath: "./data/agents.db",

    // Optional: Base directory for per-session files.
    // Default: {DBPath_dir}/sessions
    SessionDir: "./data/sessions",

    // Optional: Directory for handoff markdown files.
    // Default: {DBPath_dir}/handoffs
    HandoffDir: "./data/handoffs",

    // Optional: Per-session SSE ring buffer capacity.
    // Default: 1024
    RingBufferSize: 1024,

    // Optional: Duration before idle lifecycle handling.
    // Default: 10 minutes
    IdleTimeout: 10 * time.Minute,

    // Optional: environment for spawned agent processes.
    // Default when nil: os.Environ()
    AgentEnv: func() []string {
        return os.Environ()
    },
}
```

## Optional Interfaces

All interfaces are optional. Some features are skipped or no-op when their interface is not configured.

### AgentContextProvider / AgentRuntimeProvider

Adds per-session/provider prompt context or runtime environment after working directory resolution.

```go
type AgentContextProvider interface {
    ContextForAgent(sessionID, providerID string) string
}

type AgentRuntimeProvider interface {
    PrepareAgentRuntime(req AgentRuntimeRequest) (*AgentRuntime, error)
}
```

`AgentRuntime.Env` entries are merged into the subprocess environment. `SendRequest.Env` can add per-call environment variables for one launch.

### MCPConfigProvider

Generates MCP JSON configuration for provider subprocesses.

```go
type MCPConfigProvider interface {
    MCPConfig(sessionID, agent string) (string, error)
}
```

### ActionSummaryFormatter

Converts tool calls into human-readable action summaries.

```go
type ActionSummaryFormatter interface {
    FormatAction(tool string, args map[string]interface{}) string
}
```

Default: `DefaultActionFormatter` converts `tool_name` to `"Tool name"`.

### HandoffHandler

Called by automatic handoff trigger paths.

```go
type HandoffHandler interface {
    HandleHandoff(sessionID, summary, currentState, pendingTasks string) error
}
```

If no handler is configured, automatic triggers clear/reset session state but do not write custom external handoff output. The public `Manager.HandleHandoff(...)` helper writes markdown to `HandoffDir`.

### TitleGenerator

Generates session titles from prompts.

```go
type TitleGenerator interface {
    GenerateTitle(prompt string) string
}
```

### SummaryGenerator

```go
type SummaryGenerator interface {
    GenerateSummary(entries []ConversationEntry) (*SessionSummary, error)
}
```

The interface exists for embedder integration, but current `debounceSummary` / `generateSummary` paths are stubs. Do not rely on automatic summary generation yet.

### Transcriber

Abstracts speech-to-text.

```go
type Transcriber interface {
    Transcribe(audioPath, language string) (string, error)
}
```

A built-in `whisper.NewTranscriber()` implementation is available, but queue processing currently sends queue text directly and does not transcribe audio queue items during `ProcessNextFromQueue()`.

### ToolExecutor

Handles tool calls from HTTP-based agent loops; subprocess agents normally use MCP instead.

```go
type ToolExecutor interface {
    ExecuteTool(sessionID, toolName string, args map[string]interface{}) (string, error)
}
```

## Provider and Discovery Config

| Source | Purpose |
|--------|---------|
| `CONSTELLATION_PROVIDER_CONFIG_DIR` | Override directory for provider JSON config files |
| `CONSTELLATION_DISCOVERY_CACHE_DIR` | Override model/provider discovery cache directory |

`NewManager()` creates a `ProviderRegistry`, seeds built-in provider config files, loads provider files, validates them, and syncs enabled providers into SQLite and memory.

## Agent Process Environment

By default, subprocesses inherit `os.Environ()`. Isolation is opt-in:

```go
envProvider := mux.NewDefaultEnvProvider(baseDir)
cfg.AgentEnv = envProvider.AgentEnv
```

`DefaultEnvProvider` strips Claude Code-specific variables and sets isolated `CODEX_HOME` and `OPENCODE_CONFIG_DIR` values. If `Config.AgentEnv` is nil, these isolated values are not applied automatically.

## Whisper Configuration

| Variable | Default / Behavior | Purpose |
|----------|--------------------|---------|
| `WHISPER_BIN` | First binary checked, then `whisper-cpp`, `whisper`, `main` on PATH | Whisper executable |
| `WHISPER_MODEL` | `ggml-medium.bin` | Model filename/name |
| `WHISPER_MODEL_PATH` | Overrides model path | Full model path |

`whisper.NewTranscriber()` returns `(*Transcriber, error)`.

## Directory Structure

After initialization, the library creates:

```
{DBPath_dir}/
├── agents.db              # SQLite database
├── sessions/
│   └── {session-id}/
│       ├── conversation.jsonl   # Rich conversation log
│       ├── summary.json         # Session summary when written
│       └── attachments/
│           └── {id}_{filename}  # Uploaded files
└── handoffs/
    └── {sessionID}_{unix}.md    # Manual handoff files
```
