# Constellation

<p align="center">
  <img src="assets/brand/svg/constellation_logo_dark.svg" alt="Constellation" width="320" />
</p>

Constellation is an orchestration library for multiplexing AI agent CLIs. It publishes under the module path `github.com/apsis-ai/constellation` and currently keeps the CLI binary name `agents-mux`.

## Features

- **Provider-based multi-agent support** — Claude, Codex, OpenCode, Pi, and Cursor/Agent via configurable subprocess providers
- **Session management** — Create, list, delete sessions with SQLite persistence
- **Real-time streaming** — SSE broadcasting with ring buffer and full-flush reconnection support
- **Queue system** — Position-based follow-up queue with pause/resume, reorder/update/delete, and atomic processing
- **Conversation persistence** — Dual storage via SQLite messages table and JSONL files
- **Lifecycle management** — Idle timers, optional handoff handler, process group cleanup, stop/stop-all
- **Attachment handling** — File upload, size/count/extension validation, and resolution for agent prompts
- **Provider registry** — DB-backed provider registry seeded from provider JSON config files
- **Speech-to-text** — Whisper integration for audio transcription
- **Optional environment isolation** — `DefaultEnvProvider` can supply isolated `CODEX_HOME` and `OPENCODE_CONFIG_DIR`; default process env is `os.Environ()` when no provider is supplied

## Installation

The public module path is `github.com/apsis-ai/constellation`.

```bash
go get github.com/apsis-ai/constellation
```

Requires Go 1.25.0+.

## Quick Start

```go
package main

import (
    "fmt"
    "log"

    mux "github.com/apsis-ai/constellation"
)

func main() {
    mgr, err := mux.NewManager(mux.Config{
        DBPath: "./data/agents.db",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer mgr.Close()

    result, err := mgr.Send(mux.SendRequest{
        Prompt:     "Hello, what can you help me with?",
        ProviderID: "claude",
    })
    if err != nil {
        log.Fatal(err)
    }

    for event := range result.Events {
        switch event.Type {
        case mux.ChanText:
            fmt.Print(event.Text)
        case mux.ChanAction:
            fmt.Printf("\n[Action] %s\n", event.Text)
        case mux.ChanAskUser:
            fmt.Printf("\n[Question] %s\n", event.Text)
        case mux.ChanUIBlock:
            fmt.Printf("\n[UI] %s\n", event.JSON)
        }
    }
}
```

## Documentation

- [Architecture](docs/architecture.md) — System design, data flow, and component overview
- [Configuration](docs/configuration.md) — Manager config, interfaces, and environment setup
- [API Reference](docs/api.md) — Public API methods and types
- [Queue System](docs/queue.md) — Follow-up queue management
- [Broadcasting](docs/broadcasting.md) — SSE event streaming and reconnection
- [Agents](docs/agents.md) — Built-in providers, parsers, and registry

## Built-in Providers

| Provider ID | CLI Binary | Default Model | Output Format |
|-------------|------------|---------------|---------------|
| `claude` | `claude` | `sonnet` | NDJSON (`assistant`/`result`) |
| `codex` | `codex` | `gpt-5.4` | NDJSON (`item.completed`/`turn.completed`) |
| `opencode` | `opencode` | configured by provider/model options | JSON (`text`/`tool_use`/`step_finish`) |
| `pi` | `pi` | `openai-codex/gpt-5.5` | JSONL (`message_update`/`tool_execution`/`turn_end`) |
| `agent` | `agent` | configured by provider/model options | NDJSON (`assistant`/`tool_call`/`result`) |

The Cursor-compatible provider uses provider ID `agent`, binary `agent`, and parser type `cursor`.

## Related Projects

- **[Perigee](https://github.com/apsis-ai/perigee)** — Apsis remote desktop workspace. This library was extracted from its session management. Both projects are co-developed and may be modified together.

## Requirements

- Go 1.25.0+
- At least one agent CLI installed on PATH (`claude`, `codex`, `opencode`, `pi`, or `agent`)
- Optional: `whisper.cpp` binary for speech-to-text

## License

MIT
