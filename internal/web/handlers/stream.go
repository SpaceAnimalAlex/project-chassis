package handlers

import (
	"fmt"
	"net/http"
	"time"
)

// streamKeepAlive is how often a comment-only frame is sent on an idle SSE
// connection, purely so a dead connection (client gone, network dropped) is
// noticed and cleaned up rather than leaking a subscriber forever.
const streamKeepAlive = 25 * time.Second

// Stream handles GET /api/items/{id}/stream — a live feed of new messages
// on this work item via Server-Sent Events. Consumed by the browser's
// native EventSource (see static/app.js); no client-side library involved.
func (h *Handlers) Stream(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	events, cancel := h.Hub.Subscribe(id)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ticker := time.NewTicker(streamKeepAlive)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Name, ev.Data)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}

// Typing handles POST /api/items/{id}/typing — a fire-and-forget, zero-DB-write
// ping broadcast to anyone else currently streaming this item. The client
// re-sends this on a throttle while the composer has focus and text (see
// app.js); the hub attaches no server-side TTL or storage, the receiving
// client just clears its own indicator if no further ping arrives within
// stream.TypingTTL.
func (h *Handlers) Typing(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}
	u := actor(r)
	h.Hub.NotifyTyping(id, u.ID, u.FullName)
	w.WriteHeader(http.StatusNoContent)
}
