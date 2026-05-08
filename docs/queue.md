# Queue System

The queue system stores follow-up prompts while a session is active and processes them sequentially after the current agent run completes.

## How It Works

1. Add an item with `AddToQueue(QueueAddRequest)`
2. Constellation assigns the next 1-based pending `position`
3. When the active agent completes, `ProcessNextFromQueue(sessionID)` runs automatically
4. The next pending item is claimed atomically in SQLite and marked `processing`
5. The item is sent through `Send()` with its provider/config/attachments/working directory
6. On success it becomes `completed`; on error it becomes `failed`
7. Processing continues until no pending items remain or the queue is paused

## Queue Item Lifecycle

```
pending ──► processing ──► completed
                │
                └──► failed (with error message)
```

## Pause/Resume

Queue processing can be paused per session:

- `Stop()` pauses the queue for that session
- Ask-user waits can pause processing until user input is handled
- `ResumeQueue(sessionID)` unpauses and triggers the next pending item

## Ordering

Pending items are ordered by `position`, starting at `1`. `ReorderQueue(sessionID, itemIDs)` rewrites pending positions according to the provided item ID order.

## Listing

- `ListQueue(sessionID)` returns pending items only
- `ListQueueAll(sessionID)` returns all statuses
- `QueueLength(sessionID)` counts pending items

## Audio Queue Items

Queue items can store `source`, `transcript`, and attachment metadata. Current queue processing sends `QueueItem.Text` through `SendRequest`; it does not automatically transcribe audio during `processQueueItem()`.

## Example

```go
first, err := mgr.AddToQueue(mux.QueueAddRequest{
    SessionID:  "session-123",
    Text:       "Now refactor the tests",
    ProviderID: "claude",
})
if err != nil {
    return err
}

second, err := mgr.AddToQueue(mux.QueueAddRequest{
    SessionID:  "session-123",
    Text:       "Then update the README",
    ProviderID: "claude",
})
if err != nil {
    return err
}

items, err := mgr.ListQueue("session-123")
if err != nil {
    return err
}
// items[0].Position == 1
// items[1].Position == 2

items, err = mgr.ReorderQueue("session-123", []string{second.ID, first.ID})
if err != nil {
    return err
}
```

## Updating and Deleting

```go
updated, err := mgr.UpdateQueueItem("session-123", first.ID, mux.QueueItemUpdate{
    Text: "Updated prompt text",
})

err = mgr.DeleteQueueItem("session-123", first.ID)
```

Only pending items are intended to be edited, deleted, or reordered.
