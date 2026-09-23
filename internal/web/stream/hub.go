// Package stream is the SSE broadcast hub for live work-item updates. It's a
// separate leaf package (same reasoning as internal/web/session) because it
// needs to be reachable from both internal/web/handlers (to serve the
// stream and accept typing pings) and cmd/chassis/main.go (to hand the same
// Hub to mailengine.WorkerConfig.Notifier) — handlers already gets imported
// by web, so a type living in web itself would create a cycle.
package stream

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/project-chassis/chassis/internal/core"
)

// Event is one SSE frame: an event name and its JSON payload.
type Event struct {
	Name string
	Data []byte
}

// TypingTTL is how long a client should treat a typing ping as current
// before assuming the other operator stopped — this is a purely client-side
// display convention (see app.js); the hub itself never stores typing state.
const TypingTTL = 2500 * time.Millisecond

// Hub fans out work-item events (new messages, typing pings) to every open
// SSE connection for that item. It holds no history — a client that wasn't
// connected when an event fired simply doesn't see it and falls back to the
// regular GET /api/items/{id} on next load, same as before Round 2 existed.
type Hub struct {
	mu   sync.Mutex
	subs map[int64]map[chan Event]struct{}
}

// NewHub creates an empty broadcast hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[int64]map[chan Event]struct{})}
}

var _ core.Notifier = (*Hub)(nil)

// Subscribe registers a new listener for a work item's events. The returned
// channel is closed, and the subscription removed, when the returned cancel
// func is called — callers must always call it (typically via defer) when
// their SSE connection ends.
func (h *Hub) Subscribe(workItemID int64) (<-chan Event, func()) {
	ch := make(chan Event, 8) // small buffer so a slow publish never blocks the writer goroutine
	h.mu.Lock()
	set, ok := h.subs[workItemID]
	if !ok {
		set = make(map[chan Event]struct{})
		h.subs[workItemID] = set
	}
	set[ch] = struct{}{}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if set, ok := h.subs[workItemID]; ok {
			if _, present := set[ch]; present {
				delete(set, ch)
				close(ch)
			}
			if len(set) == 0 {
				delete(h.subs, workItemID)
			}
		}
	}
	return ch, cancel
}

// Publish sends an event to every current subscriber of a work item.
// Non-blocking per subscriber: a stalled/slow reader gets the event dropped
// rather than stalling every other subscriber or the publisher (the mail
// sync worker, or an HTTP handler) — SSE here is a live-UI convenience, not
// a delivery guarantee, and the client always has the authoritative
// GET /api/items/{id} to fall back on.
func (h *Hub) Publish(workItemID int64, name string, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[workItemID] {
		select {
		case ch <- Event{Name: name, Data: data}:
		default:
			// subscriber's buffer is full; drop rather than block
		}
	}
}

// NotifyNewMessage implements core.Notifier — called by both the HTTP
// AddMessage handler (staff replies) and mailengine.SyncWorker (inbound
// email), so a live viewer sees either kind of reply the same way.
func (h *Hub) NotifyNewMessage(workItemID int64, msg core.ThreadMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.Publish(workItemID, "message", data)
}

// typingPing is the payload broadcast for a "someone is typing" indicator.
type typingPing struct {
	UserID   int64  `json:"user_id"`
	UserName string `json:"user_name"`
}

// NotifyTyping broadcasts an ephemeral typing indicator. Deliberately not
// part of core.Notifier — it's not a domain event, just a live-UI courtesy,
// so it stays a Hub-specific method rather than widening the storage-facing
// interface.
func (h *Hub) NotifyTyping(workItemID, userID int64, userName string) {
	data, err := json.Marshal(typingPing{UserID: userID, UserName: userName})
	if err != nil {
		return
	}
	h.Publish(workItemID, "typing", data)
}
