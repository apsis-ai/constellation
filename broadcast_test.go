package mux

import (
	"testing"
	"time"
)

func TestRingBuffer_PushAndAll(t *testing.T) {
	rb := NewRingBuffer(10)
	for i := uint64(1); i <= 5; i++ {
		rb.Push(SessionStreamEvent{Seq: i, Event: SSEChunk, Data: map[string]interface{}{"n": i}})
	}
	all := rb.All()
	if len(all) != 5 {
		t.Fatalf("expected 5 events, got %d", len(all))
	}
	for i, e := range all {
		if e.Seq != uint64(i+1) {
			t.Errorf("event %d: expected seq %d, got %d", i, i+1, e.Seq)
		}
	}
}

func TestRingBuffer_WrapAround(t *testing.T) {
	rb := NewRingBuffer(4)
	for i := uint64(1); i <= 6; i++ {
		rb.Push(SessionStreamEvent{Seq: i, Event: SSEChunk})
	}
	all := rb.All()
	if len(all) != 4 {
		t.Fatalf("expected 4 events after wrap, got %d", len(all))
	}
	// Should have events 3,4,5,6 (oldest 2 evicted)
	if all[0].Seq != 3 {
		t.Errorf("expected oldest seq 3, got %d", all[0].Seq)
	}
	if all[3].Seq != 6 {
		t.Errorf("expected newest seq 6, got %d", all[3].Seq)
	}
}

func TestRingBuffer_EventsAfter_DeltaReplay(t *testing.T) {
	rb := NewRingBuffer(10)
	for i := uint64(1); i <= 5; i++ {
		rb.Push(SessionStreamEvent{Seq: i, Event: SSEChunk})
	}
	events, ok := rb.EventsAfter(3)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events after seq 3, got %d", len(events))
	}
	if events[0].Seq != 4 {
		t.Errorf("expected seq 4, got %d", events[0].Seq)
	}
	if events[1].Seq != 5 {
		t.Errorf("expected seq 5, got %d", events[1].Seq)
	}
}

func TestRingBuffer_EventsAfter_GapDetection(t *testing.T) {
	rb := NewRingBuffer(4)
	for i := uint64(1); i <= 6; i++ {
		rb.Push(SessionStreamEvent{Seq: i, Event: SSEChunk})
	}
	// Requesting seq 1 which has been evicted (oldest is 3)
	_, ok := rb.EventsAfter(1)
	if ok {
		t.Error("expected ok=false when requested seq is older than buffer")
	}
}

func TestRingBuffer_EventsAfter_Zero(t *testing.T) {
	rb := NewRingBuffer(10)
	for i := uint64(1); i <= 3; i++ {
		rb.Push(SessionStreamEvent{Seq: i, Event: SSEChunk})
	}
	events, ok := rb.EventsAfter(0)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events after seq 0, got %d", len(events))
	}
}

func TestRingBuffer_Empty(t *testing.T) {
	rb := NewRingBuffer(10)
	events, ok := rb.EventsAfter(0)
	if !ok {
		t.Fatal("expected ok=true for empty buffer")
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events from empty buffer, got %d", len(events))
	}
	all := rb.All()
	if len(all) != 0 {
		t.Errorf("expected 0 from All on empty buffer, got %d", len(all))
	}
}

func TestBroadcaster_SubscribeAndPublish(t *testing.T) {
	b := NewSessionBroadcaster(10)
	events, done, _, _ := b.SubscribeSession("s1", 0)
	defer close(done)

	// Publish 5 events
	for i := 0; i < 5; i++ {
		b.PublishSessionEvent("s1", SSEChunk, map[string]interface{}{"i": i})
	}

	// Read them back
	for i := 0; i < 5; i++ {
		select {
		case e := <-events:
			if e.Event != SSEChunk {
				t.Errorf("expected SSEChunk, got %s", e.Event)
			}
			if e.Seq != uint64(i+1) {
				t.Errorf("expected seq %d, got %d", i+1, e.Seq)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for event %d", i)
		}
	}
}

func TestBroadcaster_DeltaReplay(t *testing.T) {
	b := NewSessionBroadcaster(10)

	// Publish 5 events before subscriber connects
	for i := 0; i < 5; i++ {
		b.PublishSessionEvent("s1", SSEChunk, map[string]interface{}{"i": i})
	}

	// Subscribe with lastSeq=3 to get delta replay
	_, done, replay, fullFlush := b.SubscribeSession("s1", 3)
	defer close(done)

	if fullFlush {
		t.Error("expected delta replay, not full flush")
	}
	if len(replay) != 2 {
		t.Fatalf("expected 2 replay events, got %d", len(replay))
	}
	if replay[0].Seq != 4 {
		t.Errorf("expected replay[0].Seq=4, got %d", replay[0].Seq)
	}
	if replay[1].Seq != 5 {
		t.Errorf("expected replay[1].Seq=5, got %d", replay[1].Seq)
	}
}

func TestBroadcaster_FullFlushOnGap(t *testing.T) {
	b := NewSessionBroadcaster(4)

	// Publish 6 events, ring buffer wraps (oldest kept is seq 3)
	for i := 0; i < 6; i++ {
		b.PublishSessionEvent("s1", SSEChunk, map[string]interface{}{"i": i})
	}

	// Subscribe with lastSeq=1 which is no longer in buffer
	_, done, _, fullFlush := b.SubscribeSession("s1", 1)
	defer close(done)

	if !fullFlush {
		t.Error("expected full flush when gap detected")
	}
}

func TestBroadcaster_NotifySubscribeAndPublish(t *testing.T) {
	b := NewSessionBroadcaster(10)
	ch, done := b.SubscribeNotify()
	defer close(done)

	b.PublishSessionCreated("s1", "Test Session")

	select {
	case evt := <-ch:
		if evt.Event != NotifySessionCreated {
			t.Errorf("expected session_created, got %s", evt.Event)
		}
		if evt.Data["session_id"] != "s1" {
			t.Errorf("expected session_id 's1', got %v", evt.Data["session_id"])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for notify event")
	}
}

func TestBroadcaster_PublishStatus_BothStreams(t *testing.T) {
	b := NewSessionBroadcaster(10)
	sessCh, sessDone, _, _ := b.SubscribeSession("s1", 0)
	defer close(sessDone)
	notifyCh, notifyDone := b.SubscribeNotify()
	defer close(notifyDone)

	b.PublishStatus("s1", "active", "Thinking...", "bash", "hello", 2, false)

	// Session subscriber should get status event
	select {
	case e := <-sessCh:
		if e.Event != SSEStatus {
			t.Errorf("expected SSEStatus on session stream, got %s", e.Event)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout on session stream")
	}

	// Notify subscriber should also get status event
	select {
	case e := <-notifyCh:
		if e.Event != NotifyStatus {
			t.Errorf("expected NotifyStatus on notify stream, got %s", e.Event)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout on notify stream")
	}
}

func TestBroadcaster_PublishSessionDeleted(t *testing.T) {
	b := NewSessionBroadcaster(10)
	ch, done := b.SubscribeNotify()
	defer close(done)

	b.PublishSessionDeleted("s1")

	select {
	case evt := <-ch:
		if evt.Event != NotifySessionDeleted {
			t.Errorf("expected session_deleted, got %s", evt.Event)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for delete notify")
	}
}

func TestBroadcaster_CurrentSeq(t *testing.T) {
	b := NewSessionBroadcaster(10)
	if seq := b.CurrentSeq("s1"); seq != 0 {
		t.Errorf("expected 0 for new session, got %d", seq)
	}
	b.PublishSessionEvent("s1", SSEChunk, nil)
	b.PublishSessionEvent("s1", SSEChunk, nil)
	if seq := b.CurrentSeq("s1"); seq != 2 {
		t.Errorf("expected seq 2, got %d", seq)
	}
}

func TestBroadcaster_RingEventsAfter(t *testing.T) {
	b := NewSessionBroadcaster(10)
	b.PublishSessionEvent("s1", SSEChunk, map[string]interface{}{"n": 1})
	b.PublishSessionEvent("s1", SSEChunk, map[string]interface{}{"n": 2})
	b.PublishSessionEvent("s1", SSEChunk, map[string]interface{}{"n": 3})

	events, ok := b.RingEventsAfter("s1", 1)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
}

func TestBroadcaster_NilSafety(t *testing.T) {
	var b *SessionBroadcaster

	// All methods should be safe on nil
	b.PublishSessionEvent("s1", SSEChunk, nil)
	b.PublishNotify(NotifyStatus, nil)
	b.PublishStatus("s1", "idle", "", "", "", 0, false)
	b.PublishSessionCreated("s1", "")
	b.PublishSessionDeleted("s1")

	if seq := b.CurrentSeq("s1"); seq != 0 {
		t.Errorf("expected 0 on nil, got %d", seq)
	}
	events, ok := b.RingEventsAfter("s1", 0)
	if !ok {
		t.Error("expected ok=true on nil")
	}
	if len(events) != 0 {
		t.Error("expected empty events on nil")
	}
}

func TestSessionStreamEvent_FormatSSE(t *testing.T) {
	e := SessionStreamEvent{
		Seq:   42,
		Event: SSEChunk,
		Data:  map[string]interface{}{"content": "hello"},
	}
	s := e.FormatSSE()
	if s == "" {
		t.Error("expected non-empty SSE output")
	}
	// Should contain id, event, and data fields
	if !contains(s, "id: 42") {
		t.Error("expected 'id: 42' in SSE output")
	}
	if !contains(s, "event: chunk") {
		t.Error("expected 'event: chunk' in SSE output")
	}
}

func TestNotifyEvent_FormatSSE(t *testing.T) {
	e := NotifyEvent{
		Event: NotifySessionCreated,
		Data:  map[string]interface{}{"session_id": "s1"},
	}
	s := e.FormatSSE()
	if !contains(s, "event: session_created") {
		t.Error("expected 'event: session_created' in SSE output")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestBroadcaster_MultipleSubscribers(t *testing.T) {
	b := NewSessionBroadcaster(10)
	ch1, done1, _, _ := b.SubscribeSession("s1", 0)
	defer close(done1)
	ch2, done2, _, _ := b.SubscribeSession("s1", 0)
	defer close(done2)

	b.PublishSessionEvent("s1", SSEChunk, map[string]interface{}{"msg": "hello"})

	for _, ch := range []<-chan SessionStreamEvent{ch1, ch2} {
		select {
		case e := <-ch:
			if e.Seq != 1 {
				t.Errorf("expected seq 1, got %d", e.Seq)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for event")
		}
	}
}

func TestBroadcaster_UnsubscribeOnDoneClose(t *testing.T) {
	b := NewSessionBroadcaster(10)
	_, done, _, _ := b.SubscribeSession("s1", 0)

	// Close done to unsubscribe
	close(done)
	time.Sleep(50 * time.Millisecond) // Let goroutine clean up

	// Publishing should not panic or deadlock
	b.PublishSessionEvent("s1", SSEChunk, nil)
}
