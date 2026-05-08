# Broadcasting

The broadcasting system provides real-time event streaming with per-session ring buffers for reconnect support and a global notify channel for session lifecycle events.

## Session Events

Each session has its own event stream. Event constants include:

| Event Type | Description | Blocking publish |
|------------|-------------|------------------|
| `message` | Message record event | No |
| `chunk` | Text output from agent | No |
| `ack` | Prompt/message acknowledgement | No |
| `action` | Tool/action status update | No |
| `done` | Agent/session completed | Yes |
| `error` | Agent/session errored | Yes |
| `status` | Session status changed | Yes |
| `flush_done` | Full-state flush completed | No |
| UI block events | Structured UI card/block events | No |
| `ui_response` | User response to a UI block | No |

Ask-user prompts are carried through message/UI-block data rather than a dedicated blocking SSE event type.

## Subscribing

```go
broadcaster := mgr.GetBroadcaster()

events, done, replay, fullFlush := broadcaster.SubscribeSession("session-123", lastSeq)
defer close(done)

if fullFlush {
    refreshFullState()
}
for _, event := range replay {
    handleEvent(event)
}
for event := range events {
    handleEvent(event)
}
```

`SubscribeSession(sessionID string, lastSeq uint64)` returns:

1. live event channel
2. `done` channel; close it to unsubscribe
3. replay events after `lastSeq`, when available
4. `fullFlush` flag when the client should reload state instead of applying replay only

`SubscribeSessionWithSeq(sessionID string, lastSeq uint64)` returns the same values plus the session sequence captured while the subscriber was attached. HTTP handlers that perform a full state flush can use that snapshot as the `flush_done` high-water mark; events with higher sequence numbers are already queued on the live channel.

## Ring Buffer & Reconnection

Each session maintains a fixed-size ring buffer with monotonic sequence numbers.

- `lastSeq == 0`: request a full flush (`fullFlush = true`) and no replay
- events after `lastSeq` are in buffer: replay them, then stream live events
- `lastSeq` is older than buffer contents or ahead of current server seq: request a full flush

This allows reconnect without per-client server state while preserving correctness after gaps or restarts.

## Notify Stream

A global notification stream broadcasts session-level events:

```go
notifications, done := broadcaster.SubscribeNotify()
defer close(done)

for event := range notifications {
    // event.Type: session/status-style notification
    // event.SessionID: affected session
}
```

`SubscribeNotify()` returns a notification channel and a `done` channel for unsubscribe.

## Non-blocking vs Blocking

Most event publishing is non-blocking so slow subscribers do not stall agent output. `PublishSessionEvent()` uses blocking delivery only for `done`, `error`, and `status` events to improve terminal/status delivery.

## Concurrency

The broadcaster is safe for concurrent publishers and subscribers. Session buffers, subscriber maps, and notify subscribers are guarded by locks.
