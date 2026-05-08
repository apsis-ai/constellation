# Architecture

## Overview

Constellation is built around a central `Manager` that coordinates session lifecycle, provider subprocess management, event broadcasting, queue processing, and persistence. State is persisted in SQLite with per-session JSONL conversation logs. The Go module path is `github.com/apsis-ai/constellation`; the compatibility CLI binary name remains `agents-mux`.

## Core Components

```
┌────────────────────────────────────────────────────────────┐
│                         Manager                             │
│  ┌──────────┐  ┌──────────┐  ┌───────────────────────────┐ │
│  │ SQLite DB│  │ Processes│  │ SessionBroadcaster         │ │
│  └──────────┘  └──────────┘  └───────────────────────────┘ │
│  ┌──────────┐  ┌──────────┐  ┌───────────────────────────┐ │
│  │ Queue    │  │ Lifecycle│  │ ProviderRegistry          │ │
│  └──────────┘  └──────────┘  └───────────────────────────┘ │
└────────────────────────────────────────────────────────────┘
        │                         │
        ▼                         ▼
 Provider subprocess          SSE/notify subscribers
```

### Manager (`session.go`)

Owns the SQLite database, broadcaster, provider registry, parser registry, process tracking maps, idle timers, session directories, and optional embedder interfaces.

### Provider Registry (`provider_registry.go`)

DB-backed registry of provider configs. Built-ins are seeded to provider JSON files, loaded, validated, and synchronized into SQLite/cache. Enabled CLI providers are used by `Send()`.

### Provider Spawner (`agent.go`, `cli_provider.go`)

Spawns provider CLI processes with:

- process group isolation (`Setpgid: true`) for child cleanup
- provider-specific command construction from `CLIProviderConfig`
- optional runtime context/env and MCP config
- working directory resolution and validation
- normalized parser callbacks

Environment isolation is optional: by default subprocesses inherit `os.Environ()` unless `Config.AgentEnv` supplies a custom environment such as `DefaultEnvProvider.AgentEnv()`.

### Stream Parsers (`parser_*.go`)

Parsers normalize CLI output into `ChanEvent` values:

- **Claude**: `assistant` / `result`
- **Codex**: `item.completed`, `item.delta`, `turn.completed`
- **OpenCode**: `text`, `tool_use`, `step_finish`, `error`
- **Pi**: `message_update`, `tool_execution_*`, `turn_end`, session header events; `thinking_delta` updates become phased `rationale_summary` UI blocks
- **Cursor-compatible Agent**: `assistant`, `tool_call`, `result`, `error`; `thinking` events become phased `rationale_summary` UI blocks

### SessionBroadcaster (`broadcast.go`)

Fans out per-session events with sequenced ring buffers and global notify events. `lastSeq == 0` requests a full flush; gaps also request full flush.

### Queue System (`queue.go`)

Position-based follow-up queue with 1-based pending positions, transactional item claiming, pause/resume, reorder, update, delete, and status tracking. Queue processing sends stored item text through `Send()`; it does not transcribe audio during processing.

### Lifecycle (`lifecycle.go`, `agent.go`)

Handles stop/stop-all, process group cleanup, screenshot tracking, idle timers, and handoff. Automatic handoff triggers call a configured `HandoffHandler` when present and then clear conversation state. Public `HandleHandoff(sessionID, summary, currentState, pendingTasks)` writes markdown to `HandoffDir`.

### Conversation Persistence (`conversation.go`)

Dual storage:

- SQLite `messages` table for indexed message lookup
- JSONL `conversation.jsonl` for rich entries with message IDs, tool calls, attachments, and metadata

### Attachments (`attachment.go`)

Uploads are saved under the session attachment directory. Validation covers upload count, filename extension, and size. Attachment type is derived from filename extension; MIME validation is not performed by the manager.

## Data Flow

### Prompt Execution

```
SendRequest
    │
    ▼
Manager.Send()
    ├─ default/generate session, provider, user message, response IDs
    ├─ persist user message to DB + JSONL
    ├─ resolve working directory and attachments
    ├─ validate provider ConfigValues
    ├─ look up CLI provider in ProviderRegistry
    ├─ build command args/env/cwd
    ├─ spawn subprocess with process group
    ▼
Provider stdout parser
    ├─ ChanText / ChanAction / ChanAskUser / ChanUIBlock
    └─ stream result metadata
    ▼
Completion
    ├─ persist assistant message
    ├─ update conversation_id, provider/config metadata, token usage
    ├─ release I/O lock and update status
    ├─ trigger optional handoff if needed
    └─ process next queue item
```

### Reconnection

```
Client connects with lastSeq
    │
    ├─ lastSeq == 0 ───────► full flush
    ├─ events in buffer ───► replay delta then live stream
    └─ gap/restart ────────► full flush
```

## Database Schema Highlights

### `sessions`

Includes `id`, `status`, `last_active_at`, `conversation_id`, `token_usage`, `screenshot_count`, `title`, `last_agent`, `last_agent_sub`, `last_model`, `last_effort`, `provider_id`, `config_values_json`, `pid`, and `working_directory`.

### `messages`

Includes autoincrement `id`, stable `message_id`, `session_id`, `role`, `content`, and `created_at`.

### `follow_up_queue`

Includes `id`, `session_id`, `text`, `position`, legacy agent/model fields, `attachments`, `created_at`, `source`, `status`, `transcript`, `message_id`, `response_id`, `started_at`, `completed_at`, `error`, `working_directory`, `provider_id`, and `config_values_json`.

### `providers`

Stores provider ID, name, type, parser type, enabled state, priority, serialized config JSON, and timestamps.

## Design Decisions

- **Pure-Go SQLite** (`modernc.org/sqlite`): no CGO dependency for persistence
- **Provider registry**: built-ins plus file/DB-backed customization without hard-coding Send paths
- **Process groups**: `Setpgid: true` + kill `-pid` ensures child process cleanup
- **Ring buffer**: bounded memory for SSE replay and reconnect correctness
- **Per-session file mutexes**: serialize JSONL appends
- **Idempotent migrations**: `ALTER TABLE ADD COLUMN` with tolerated existing-column failures
- **Interface injection**: optional embedder hooks for MCP config, context/runtime, actions, handoff, titles, summaries, transcription, tool execution, I/O locking, and filesystem access
