package mux

import (
	"encoding/json"
	"fmt"
	"sync"
)

// --- Event Types ---

// SessionStreamEventType identifies event types on the per-session SSE stream.
type SessionStreamEventType string

const (
	SSEMessage   SessionStreamEventType = "message"
	SSEChunk     SessionStreamEventType = "chunk"
	SSEAck       SessionStreamEventType = "ack"
	SSEAction    SessionStreamEventType = "action"
	SSEDone      SessionStreamEventType = "done"
	SSEError     SessionStreamEventType = "error"
	SSEStatus    SessionStreamEventType = "status"
	SSEFlushDone SessionStreamEventType = "flush_done"
)

// SessionStreamEvent is a sequenced event on a per-session SSE stream.
type SessionStreamEvent struct {
	Seq       uint64                 `json:"seq"`
	Event     SessionStreamEventType `json:"event"`
	SessionID string                 `json:"session_id,omitempty"`
	Data      map[string]interface{} `json:"data"`
}

// FormatSSE returns the event as SSE text with id: field for reconnection.
func (e SessionStreamEvent) FormatSSE() string {
	payload := make(map[string]interface{}, len(e.Data)+1)
	for k, v := range e.Data {
		payload[k] = v
	}
	payload["seq"] = e.Seq
	data, _ := json.Marshal(payload)
	return fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Event, data)
}

// NotifyEventType identifies event types on the global notify SSE stream.
type NotifyEventType string

const (
	NotifyStatus         NotifyEventType = "status"
	NotifySessionCreated NotifyEventType = "session_created"
	NotifySessionDeleted NotifyEventType = "session_deleted"
	NotifyQueue          NotifyEventType = "queue"
)

// NotifyEvent is a global notification event (no sequence numbers).
type NotifyEvent struct {
	Event NotifyEventType        `json:"event"`
	Data  map[string]interface{} `json:"data"`
}

// FormatSSE returns the notify event as SSE text.
func (e NotifyEvent) FormatSSE() string {
	data, _ := json.Marshal(e.Data)
	return fmt.Sprintf("event: %s\ndata: %s\n\n", e.Event, data)
}

// --- Ring Buffer ---

// RingBuffer stores recent SessionStreamEvents for reconnection delta replay.
type RingBuffer struct {
	buf  []SessionStreamEvent
	size int
	head int
	len  int
}

// NewRingBuffer creates a ring buffer with the given capacity.
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = 1024
	}
	return &RingBuffer{
		buf:  make([]SessionStreamEvent, size),
		size: size,
	}
}

// Push adds an event to the ring buffer.
func (r *RingBuffer) Push(event SessionStreamEvent) {
	r.buf[r.head] = event
	r.head = (r.head + 1) % r.size
	if r.len < r.size {
		r.len++
	}
}

// EventsAfter returns all events with seq > afterSeq.
func (r *RingBuffer) EventsAfter(afterSeq uint64) ([]SessionStreamEvent, bool) {
	if r.len == 0 {
		return nil, true
	}
	oldestIdx := (r.head - r.len + r.size) % r.size
	oldestSeq := r.buf[oldestIdx].Seq
	if afterSeq > 0 && afterSeq < oldestSeq {
		return nil, false
	}
	var result []SessionStreamEvent
	for i := 0; i < r.len; i++ {
		idx := (oldestIdx + i) % r.size
		if r.buf[idx].Seq > afterSeq {
			result = append(result, r.buf[idx])
		}
	}
	return result, true
}

// All returns all events in the buffer in order.
func (r *RingBuffer) All() []SessionStreamEvent {
	if r.len == 0 {
		return nil
	}
	result := make([]SessionStreamEvent, r.len)
	oldestIdx := (r.head - r.len + r.size) % r.size
	for i := 0; i < r.len; i++ {
		result[i] = r.buf[(oldestIdx+i)%r.size]
	}
	return result
}

// --- Per-session fanout ---

type sessionSSEClient struct {
	ch   chan SessionStreamEvent
	done chan struct{}
}

type sessionFanout struct {
	seq     uint64
	clients map[*sessionSSEClient]struct{}
	ring    *RingBuffer
}

func newSessionFanout(ringSize int) *sessionFanout {
	return &sessionFanout{
		clients: make(map[*sessionSSEClient]struct{}),
		ring:    NewRingBuffer(ringSize),
	}
}

// --- Notify clients ---

type notifyClient struct {
	ch   chan NotifyEvent
	done chan struct{}
}

// --- SessionBroadcaster ---

// SessionBroadcaster manages per-session SSE fanout and global notify SSE.
type SessionBroadcaster struct {
	mu            sync.RWMutex
	sessions      map[string]*sessionFanout
	notifyClients map[*notifyClient]struct{}
	ringSize      int
}

// NewSessionBroadcaster creates a broadcaster.
func NewSessionBroadcaster(ringSize ...int) *SessionBroadcaster {
	rs := 1024
	if len(ringSize) > 0 && ringSize[0] > 0 {
		rs = ringSize[0]
	}
	return &SessionBroadcaster{
		sessions:      make(map[string]*sessionFanout),
		notifyClients: make(map[*notifyClient]struct{}),
		ringSize:      rs,
	}
}

func (b *SessionBroadcaster) getOrCreateSession(sessionID string) *sessionFanout {
	f, ok := b.sessions[sessionID]
	if !ok {
		f = newSessionFanout(b.ringSize)
		b.sessions[sessionID] = f
	}
	return f
}

// SubscribeSession subscribes to a specific session's event stream.
func (b *SessionBroadcaster) SubscribeSession(sessionID string, lastSeq uint64) (events <-chan SessionStreamEvent, done chan struct{}, replay []SessionStreamEvent, fullFlush bool) {
	if b == nil {
		ch := make(chan SessionStreamEvent)
		d := make(chan struct{})
		close(ch)
		close(d)
		return ch, d, nil, false
	}
	b.mu.Lock()
	f := b.getOrCreateSession(sessionID)
	c := &sessionSSEClient{
		ch:   make(chan SessionStreamEvent, 64),
		done: make(chan struct{}),
	}
	f.clients[c] = struct{}{}
	if lastSeq == 0 {
		fullFlush = true
		replay = nil
	} else if lastSeq > 0 && lastSeq > f.seq {
		fullFlush = true
		replay = nil
	} else {
		var ok bool
		replay, ok = f.ring.EventsAfter(lastSeq)
		if !ok {
			fullFlush = true
			replay = nil
		}
	}
	b.mu.Unlock()
	go func() {
		<-c.done
		b.mu.Lock()
		delete(f.clients, c)
		b.mu.Unlock()
	}()
	return c.ch, c.done, replay, fullFlush
}

// SubscribeNotify subscribes to global notification events.
func (b *SessionBroadcaster) SubscribeNotify() (<-chan NotifyEvent, chan struct{}) {
	if b == nil {
		ch := make(chan NotifyEvent)
		done := make(chan struct{})
		close(ch)
		close(done)
		return ch, done
	}
	c := &notifyClient{
		ch:   make(chan NotifyEvent, 32),
		done: make(chan struct{}),
	}
	b.mu.Lock()
	b.notifyClients[c] = struct{}{}
	b.mu.Unlock()
	go func() {
		<-c.done
		b.mu.Lock()
		delete(b.notifyClients, c)
		b.mu.Unlock()
	}()
	return c.ch, c.done
}

// PublishSessionEvent publishes a sequenced event to a specific session's subscribers.
func (b *SessionBroadcaster) PublishSessionEvent(sessionID string, eventType SessionStreamEventType, data map[string]interface{}) uint64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	f := b.getOrCreateSession(sessionID)
	f.seq++
	event := SessionStreamEvent{
		Seq:       f.seq,
		Event:     eventType,
		SessionID: sessionID,
		Data:      data,
	}
	f.ring.Push(event)
	clients := make([]*sessionSSEClient, 0, len(f.clients))
	for c := range f.clients {
		clients = append(clients, c)
	}
	b.mu.Unlock()
	isTerminal := eventType == SSEDone || eventType == SSEError || eventType == SSEStatus
	for _, c := range clients {
		if isTerminal {
			select {
			case c.ch <- event:
			case <-c.done:
			}
		} else {
			select {
			case c.ch <- event:
			default:
			}
		}
	}
	return event.Seq
}

// PublishNotify publishes a global notification event.
func (b *SessionBroadcaster) PublishNotify(eventType NotifyEventType, data map[string]interface{}) {
	if b == nil {
		return
	}
	b.mu.Lock()
	clients := make([]*notifyClient, 0, len(b.notifyClients))
	for c := range b.notifyClients {
		clients = append(clients, c)
	}
	b.mu.Unlock()
	event := NotifyEvent{Event: eventType, Data: data}
	for _, c := range clients {
		select {
		case c.ch <- event:
		default:
		}
	}
}

// PublishChunk publishes a streaming text chunk to a session.
func (b *SessionBroadcaster) PublishChunk(sessionID, messageID, content string) uint64 {
	return b.PublishSessionEvent(sessionID, SSEChunk, map[string]interface{}{
		"message_id": messageID,
		"content":    content,
	})
}

// PublishAck publishes an ack event mapping client_id to server id.
func (b *SessionBroadcaster) PublishAck(sessionID, clientID, serverID string) uint64 {
	return b.PublishSessionEvent(sessionID, SSEAck, map[string]interface{}{
		"client_id": clientID,
		"id":        serverID,
	})
}

// PublishAction publishes a tool-use action event to a session.
func (b *SessionBroadcaster) PublishAction(sessionID, messageID string, actionData map[string]interface{}) uint64 {
	data := map[string]interface{}{"message_id": messageID}
	for k, v := range actionData {
		data[k] = v
	}
	return b.PublishSessionEvent(sessionID, SSEAction, data)
}

// PublishDone publishes a done event to a session.
func (b *SessionBroadcaster) PublishDone(sessionID, messageID string) uint64 {
	return b.PublishSessionEvent(sessionID, SSEDone, map[string]interface{}{
		"message_id": messageID,
	})
}

// PublishError publishes an error event to a session.
func (b *SessionBroadcaster) PublishError(sessionID, messageID, errMsg string) uint64 {
	return b.PublishSessionEvent(sessionID, SSEError, map[string]interface{}{
		"message_id": messageID,
		"error":      errMsg,
	})
}

// PublishStatus publishes a status event to BOTH session SSE and notify SSE.
func (b *SessionBroadcaster) PublishStatus(sessionID, status, summary, tool, userMessage string, queueLength int, queuePaused bool) uint64 {
	data := map[string]interface{}{
		"session_id":   sessionID,
		"status":       status,
		"summary":      summary,
		"tool":         tool,
		"user_message": userMessage,
	}
	if queueLength >= 0 {
		data["queue_length"] = queueLength
		data["queue_paused"] = queuePaused
	}
	seq := b.PublishSessionEvent(sessionID, SSEStatus, data)
	b.PublishNotify(NotifyStatus, data)
	return seq
}

// PublishSessionCreated publishes a session_created event via notify SSE.
func (b *SessionBroadcaster) PublishSessionCreated(sessionID, title string) {
	b.PublishNotify(NotifySessionCreated, map[string]interface{}{
		"session_id": sessionID,
		"title":      title,
	})
}

// PublishSessionDeleted publishes a session_deleted event via notify SSE.
func (b *SessionBroadcaster) PublishSessionDeleted(sessionID string) {
	b.PublishNotify(NotifySessionDeleted, map[string]interface{}{
		"session_id": sessionID,
	})
}

// CurrentSeq returns the current sequence number for a session.
func (b *SessionBroadcaster) CurrentSeq(sessionID string) uint64 {
	if b == nil {
		return 0
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if f, ok := b.sessions[sessionID]; ok {
		return f.seq
	}
	return 0
}

// RingEventsAfter returns ring buffer events with seq > afterSeq for a session.
func (b *SessionBroadcaster) RingEventsAfter(sessionID string, afterSeq uint64) ([]SessionStreamEvent, bool) {
	if b == nil {
		return nil, true
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	f, ok := b.sessions[sessionID]
	if !ok {
		return nil, true
	}
	return f.ring.EventsAfter(afterSeq)
}
