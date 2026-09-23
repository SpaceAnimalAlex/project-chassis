// Package presence tracks which operators are currently viewing or drafting
// a reply on a work item, so a second operator gets a lock warning instead
// of silently duplicating labor.
package presence

import (
	"sync"
	"time"
)

// Mode describes what an operator is doing on a work item.
type Mode string

const (
	ModeViewing  Mode = "VIEWING"
	ModeDrafting Mode = "DRAFTING"
)

// TTL is how long a heartbeat stays valid before the viewer is considered gone.
// Clients are expected to heartbeat well inside this window (e.g. every 10s).
const TTL = 30 * time.Second

// Viewer is a single operator's presence on a work item.
type Viewer struct {
	UserID   int64     `json:"user_id"`
	UserName string    `json:"user_name"`
	Mode     Mode      `json:"mode"`
	LastSeen time.Time `json:"-"`
}

type entry struct {
	viewer Viewer
}

// Tracker is a thread-safe, in-memory presence registry.
// It is deliberately not persisted: presence is a live UI courtesy, not
// operational history, and must never survive a restart.
type Tracker struct {
	mu    sync.Mutex
	items map[int64]map[int64]entry // work_item_id -> user_id -> entry
}

// NewTracker creates an empty presence tracker.
func NewTracker() *Tracker {
	return &Tracker{items: make(map[int64]map[int64]entry)}
}

// Heartbeat records (or refreshes) a viewer's presence on a work item.
func (t *Tracker) Heartbeat(workItemID, userID int64, userName string, mode Mode) {
	t.mu.Lock()
	defer t.mu.Unlock()

	viewers, ok := t.items[workItemID]
	if !ok {
		viewers = make(map[int64]entry)
		t.items[workItemID] = viewers
	}
	viewers[userID] = entry{viewer: Viewer{
		UserID:   userID,
		UserName: userName,
		Mode:     mode,
		LastSeen: time.Now(),
	}}
}

// Release removes a viewer's presence immediately (e.g. on tab close/navigate away).
func (t *Tracker) Release(workItemID, userID int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if viewers, ok := t.items[workItemID]; ok {
		delete(viewers, userID)
		if len(viewers) == 0 {
			delete(t.items, workItemID)
		}
	}
}

// Others returns the current non-expired viewers of a work item, excluding excludeUserID.
// This is what powers the collision-prevention lock warning in the UI.
func (t *Tracker) Others(workItemID, excludeUserID int64) []Viewer {
	t.mu.Lock()
	defer t.mu.Unlock()

	viewers, ok := t.items[workItemID]
	if !ok {
		return nil
	}

	now := time.Now()
	result := make([]Viewer, 0, len(viewers))
	for uid, e := range viewers {
		if uid == excludeUserID {
			continue
		}
		if now.Sub(e.viewer.LastSeen) > TTL {
			continue
		}
		result = append(result, e.viewer)
	}
	return result
}

// Sweep evicts every viewer whose heartbeat has expired across all work items.
// Call this periodically (e.g. every TTL) from a background goroutine so the
// map doesn't grow unbounded from operators who close their browser without
// releasing.
func (t *Tracker) Sweep() {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	for itemID, viewers := range t.items {
		for uid, e := range viewers {
			if now.Sub(e.viewer.LastSeen) > TTL {
				delete(viewers, uid)
			}
		}
		if len(viewers) == 0 {
			delete(t.items, itemID)
		}
	}
}

// Run starts the periodic sweep loop and blocks until ctx-style cancellation
// via the returned stop channel is closed. Callers typically run this with `go`.
func (t *Tracker) Run(stop <-chan struct{}) {
	ticker := time.NewTicker(TTL)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			t.Sweep()
		}
	}
}
