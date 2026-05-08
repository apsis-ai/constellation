# API Reference

Constellation publishes under module path `github.com/apsis-ai/constellation`.

## Manager

### Constructor

```go
func NewManager(cfg Config) (*Manager, error)
```

Creates a manager, opens the SQLite database, creates required directories, initializes migrations, builds parser/provider registries, seeds built-in providers, and initializes broadcasters/process maps. `DBPath` is required.

### Session Operations

```go
func (m *Manager) CreateSession(sessionID string) error
func (m *Manager) ListSessions() ([]Session, error)
func (m *Manager) DeleteSession(sessionID string) error
func (m *Manager) GetMessages(sessionID string) ([]Message, error)
func (m *Manager) GetConversation(sessionID string) ([]ConversationEntry, error)
func (m *Manager) GetSummary(sessionID string) SessionSummary
func (m *Manager) Close() error
```

`CreateSession` generates a UUID when `sessionID` is empty. Message reads prefer JSONL conversation files and fall back to the SQLite `messages` table where applicable.

## Agent Control

```go
func (m *Manager) Send(req SendRequest) (*SendResult, error)
```

Creates/loads the session, persists the user message, resolves attachments and working directory, resolves provider config, spawns the provider subprocess, and returns an event channel.

### SendRequest

| Field | Type | Description |
|-------|------|-------------|
| `Prompt` | `string` | User message (required) |
| `SessionID` | `string` | Session to use; generated if empty |
| `ProviderID` | `string` | Provider ID; defaults to `claude` |
| `ConfigValues` | `map[string]any` | Runtime provider config values such as model/effort/sub-agent |
| `AttachmentIDs` | `[]string` | Attachment IDs to pass to the provider |
| `MessageID` | `string` | Optional caller-provided user message ID |
| `ResponseID` | `string` | Optional caller-provided response message ID |
| `WorkingDirectory` | `string` | Optional per-request working directory override |
| `Env` | `map[string]string` | Optional per-request environment variables merged into the spawned provider process |

### SendResult

| Field | Type | Description |
|-------|------|-------------|
| `Events` | `<-chan ChanEvent` | Stream of normalized provider events |
| `SessionID` | `string` | Session ID used |
| `MessageID` | `int64` | SQLite row ID for the user message |
| `UserMessageID` | `string` | Stable user message ID |
| `ResponseMessageID` | `string` | Stable assistant/response message ID |

### ChanEvent

| Field | Description |
|-------|-------------|
| `Type` | `ChanText`, `ChanAction`, `ChanAskUser`, or `ChanUIBlock` |
| `Text` | Human-readable text chunk/action/question |
| `JSON` | Raw JSON payload for structured UI/event data |
| `ToolName` / `ToolArgs` | Tool metadata for action events |
| `UIEventType` | Structured UI event subtype |

### Process Control

```go
func (m *Manager) Stop(sessionID string) error
func (m *Manager) StopAll() error
func (m *Manager) GetSessionStatus(sessionID string) string
func (m *Manager) SkipAsk(sessionID string) error
func (m *Manager) TrackScreenshot(sessionID string) error
```

`Stop` kills the active process group and marks the session idle. `StopAll` also cleans up orphaned PIDs recorded in the database. `TrackScreenshot` increments the screenshot counter and may trigger handoff through the configured handoff handler.

## Queue Operations

```go
func (m *Manager) AddToQueue(req QueueAddRequest) (*QueueItem, error)
func (m *Manager) ListQueue(sessionID string) ([]QueueItem, error)
func (m *Manager) ListQueueAll(sessionID string) ([]QueueItem, error)
func (m *Manager) UpdateQueueItem(sessionID, itemID string, update QueueItemUpdate) (*QueueItem, error)
func (m *Manager) DeleteQueueItem(sessionID, itemID string) error
func (m *Manager) ReorderQueue(sessionID string, itemIDs []string) ([]QueueItem, error)
func (m *Manager) ClearQueue(sessionID string) error
func (m *Manager) ResumeQueue(sessionID string) error
func (m *Manager) ProcessNextFromQueue(sessionID string) error
func (m *Manager) QueueLength(sessionID string) int
```

`ListQueue` returns pending items only. `ListQueueAll` returns pending, processing, completed, and failed items. Positions are 1-based among pending queue items.

## Lifecycle / Handoff

```go
func (m *Manager) HandleHandoff(sessionID, summary, currentState, pendingTasks string) error
```

Manual handoff writes a markdown file named `{sessionID}_{unix}.md` in `HandoffDir` and updates session state. Automatic trigger paths call the configured `HandoffHandler` when one is provided, then clear conversation state for the session.

## Attachments

```go
func (m *Manager) SaveAttachmentBytes(sessionID string, uploads []AttachmentUpload) ([]AttachmentRef, error)
func (m *Manager) ResolveAttachments(sessionID string, ids []string) ([]AttachmentRef, error)
```

`AttachmentUpload` contains `Filename`, `Data`, and `Size`. Validation is by count, extension, and size (`UploadConfig.Validate`). Attachment type is derived from the filename extension; there is no MIME-type validation in the manager.

## Broadcasting

```go
func (m *Manager) GetBroadcaster() *SessionBroadcaster
```

```go
func (b *SessionBroadcaster) SubscribeSession(sessionID string, lastSeq uint64) (<-chan SessionStreamEvent, chan struct{}, []SessionStreamEvent, bool)
func (b *SessionBroadcaster) SubscribeSessionWithSeq(sessionID string, lastSeq uint64) (<-chan SessionStreamEvent, chan struct{}, []SessionStreamEvent, bool, uint64)
func (b *SessionBroadcaster) SubscribeNotify() (<-chan NotifyEvent, chan struct{})
```

`SubscribeSession` returns live events, a done channel to close for unsubscribe, replay events, and a `fullFlush` flag. `lastSeq == 0` requests a full flush instead of a delta replay. `SubscribeSessionWithSeq` additionally returns the session sequence captured when the subscriber was attached, for callers that need a full-flush high-water mark.

## Registry

```go
func NewRegistry(providers *ProviderRegistry) *Registry
func (r *Registry) ListAgents() []AgentInfo
func (r *Registry) GetAgent(id string) (AgentInfo, bool)
func (r *Registry) Register(info AgentInfo)
func (r *Registry) Discover()
```

`Registry` is a compatibility view over provider data. In normal manager usage, provider lookup and config live in the DB-backed `ProviderRegistry` returned by `mgr.GetProviders()`.

## Whisper Transcriber

```go
import "github.com/apsis-ai/constellation/whisper"

t, err := whisper.NewTranscriber()
if err != nil {
    return err
}
text, err := t.Transcribe("/path/to/audio.wav", "en")
```

`NewTranscriber()` resolves a whisper binary from `WHISPER_BIN`, then `whisper-cpp`, `whisper`, and `main`. Default model is `ggml-medium.bin`; `WHISPER_MODEL` and `WHISPER_MODEL_PATH` override model resolution.
