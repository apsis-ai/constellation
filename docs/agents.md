# Agents and Providers

Constellation launches agent CLIs through DB-backed provider configs. Built-ins are seeded from `BuiltinCLIConfigs()` into provider JSON files and synchronized into the provider registry when `NewManager()` initializes.

## Built-in Providers

### Claude

- **Provider ID**: `claude`
- **Binary**: `claude`
- **Default model**: `sonnet`
- **Base args**: stream JSON output mode
- **Runtime config**: model, sub-agent, MCP config, resume conversation ID
- **Output format**: NDJSON with `assistant` and `result` events
- **Resume**: supported via provider resume flag when `conversation_id` belongs to the same provider

### Codex

- **Provider ID**: `codex`
- **Binary**: `codex`
- **Default model**: `gpt-5.4`
- **Base args**: `exec --json --dangerously-bypass-approvals-and-sandbox`
- **Runtime config**: model via `-m`, effort via `-c`
- **Output format**: NDJSON with `item.completed`, `item.delta`, and `turn.completed`
- **Resume**: not supported by the built-in config

### OpenCode

- **Provider ID**: `opencode`
- **Binary**: `opencode`
- **Base args**: `run --format json`
- **Runtime config**: session/resume via `-s`, model via `-m`, variant via `--variant`, agent via `--agent`, attachments via `--file`
- **Output format**: JSON events with `text`, `tool_use`, `step_finish`, and `error`
- **Resume**: supported

### Pi

- **Provider ID**: `pi`
- **Binary**: `pi`
- **Default model**: `openai-codex/gpt-5.5`
- **Base args**: `--mode json --print`
- **Runtime config**: session resume, model, thinking/effort
- **Output format**: JSONL with `message_update`, `tool_execution_*`, `turn_end`, and session header events
- **Resume**: supported

### Cursor-compatible Agent

- **Provider ID**: `agent`
- **Binary**: `agent`
- **Parser type**: `cursor`
- **Base args**: `-p --output-format stream-json --stream-partial-output --force --approve-mcps --trust`
- **Runtime config**: model and resume where supported by CLI config
- **MCP mode**: workspace
- **Output format**: NDJSON with `assistant`, `tool_call`, `result`, and `error`
- **Resume**: supported

## Provider Resolution

`Send()` reads `SendRequest.ProviderID` and defaults empty values to `claude`. It validates `ConfigValues`, loads the enabled CLI provider from `m.providers.GetCLIProvider(providerID)`, validates the binary, builds args/env/cwd, and starts the subprocess.

Built-in provider IDs are:

| Provider ID | Binary | Parser Type |
|-------------|--------|-------------|
| `claude` | `claude` | `claude` |
| `codex` | `codex` | `codex` |
| `opencode` | `opencode` | `opencode` |
| `pi` | `pi` | `pi` |
| `agent` | `agent` | `cursor` |

## Registry

`Registry` is a compatibility/inspection facade over provider data:

```go
registry := mux.NewRegistry(mgr.GetProviders())
registry.Discover()

for _, a := range registry.ListAgents() {
    fmt.Printf("%s: available=%v\n", a.Name, a.Available)
}
```

Provider config and enable/disable state are owned by `ProviderRegistry`; `NewRegistry` takes that registry as input.

## Process Management

Agent processes are spawned with process group isolation:

- `Setpgid: true` creates a new process group for each agent
- On stop, the entire process group is killed (`kill -pid`), ensuring MCP child servers are terminated
- PIDs are tracked in memory and in SQLite for orphan cleanup
- `StopAll()` kills active in-memory processes and orphaned PIDs recorded in the database

## Environment

By default, spawned agent processes inherit `os.Environ()`. Environment isolation is opt-in through `Config.AgentEnv` or an embedder-provided environment provider. `DefaultEnvProvider.AgentEnv()` strips Claude Code-specific variables and supplies isolated `CODEX_HOME` and `OPENCODE_CONFIG_DIR`; embedders must wire it into config for that behavior. `SendRequest.Env` merges additional environment variables into a single agent call.

## Conversation Resume

When sending to a session that previously used the same provider:

1. Manager checks for `conversation_id` and matching `last_agent`
2. If provider config supports resume, it passes resume args to the CLI
3. Otherwise it builds context from a handoff file or the last 20 messages

Switching providers does not reuse another provider's conversation ID.

## Handoff Triggers

Triggers include token usage above the threshold after agent completion, screenshot count via `TrackScreenshot()`, and idle timeout. Automatic trigger paths call the configured `HandoffHandler` when present, then reset session conversation state. The public `HandleHandoff(sessionID, summary, currentState, pendingTasks)` helper writes a markdown handoff file directly.
