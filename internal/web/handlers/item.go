package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/project-chassis/chassis/internal/core"
	"github.com/project-chassis/chassis/internal/presence"
)

// GetItem handles GET /api/items/{id} — full detail: item, dual-channel
// thread, linked assets, audit trail.
func (h *Handlers) GetItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}

	detail, err := h.Service.GetItem(r.Context(), id)
	if errors.Is(err, core.ErrItemNotFound) {
		writeError(w, http.StatusNotFound, "work item not found")
		return
	}
	if err != nil {
		h.Logger.Error("get item failed", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load work item")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// RenderItem handles GET /items/{id} — the server-rendered item detail shell.
func (h *Handlers) RenderItem(w http.ResponseWriter, r *http.Request) {
	if _, err := pathInt64(r, "id"); err != nil {
		http.Error(w, "invalid item id", http.StatusBadRequest)
		return
	}
	if err := h.Templates.ExecuteTemplate(w, "item_detail.html", nil); err != nil {
		h.Logger.Error("render item failed", "error", err)
		http.Error(w, "failed to render page", http.StatusInternalServerError)
	}
}

type transitionRequest struct {
	NewStatus core.Status `json:"new_status"`
}

// TransitionStatus handles POST /api/items/{id}/transition.
func (h *Handlers) TransitionStatus(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}

	var req transitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err = h.Service.TransitionStatus(r.Context(), id, actor(r).ID, req.NewStatus)
	switch {
	case errors.Is(err, core.ErrItemNotFound):
		writeError(w, http.StatusNotFound, "work item not found")
	case errors.Is(err, core.ErrInvalidTransition):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		h.Logger.Error("transition status failed", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to transition status")
	default:
		writeJSON(w, http.StatusOK, map[string]string{"status": string(req.NewStatus)})
	}
}

type addMessageRequest struct {
	Body       string `json:"body"`
	IsInternal bool   `json:"is_internal"`
}

// AddMessage handles POST /api/items/{id}/messages.
// IsInternal comes from the request body but the sender identity always
// comes from the authenticated actor header — a caller cannot forge an
// external requester message by passing a different sender_email, which is
// the concrete mechanism enforcing "internal notes never leak."
func (h *Handlers) AddMessage(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}

	var req addMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	u := actor(r)
	var authorID *int64
	if u.ID != 0 {
		authorID = &u.ID
	}

	msg, err := h.Service.AddThreadMessage(r.Context(), id, authorID, u.Email, req.Body, req.IsInternal, nil)
	switch {
	case errors.Is(err, core.ErrItemNotFound):
		writeError(w, http.StatusNotFound, "work item not found")
	case errors.Is(err, core.ErrEmptyMessageBody), errors.Is(err, core.ErrSenderRequired):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		h.Logger.Error("add message failed", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to add message")
	default:
		msg.AuthorName = u.FullName
		// Live viewers (see internal/web/stream) see this exactly the way
		// they'd see an inbound email — same Notifier call, different writer.
		h.Hub.NotifyNewMessage(id, *msg)
		writeJSON(w, http.StatusCreated, msg)
	}
}

type assignRequest struct {
	TargetUserID *int64 `json:"target_user_id"`
}

// AssignItem handles POST /api/items/{id}/assign.
func (h *Handlers) AssignItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}

	var req assignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err = h.Service.AssignItem(r.Context(), id, actor(r).ID, req.TargetUserID)
	switch {
	case errors.Is(err, core.ErrItemNotFound):
		writeError(w, http.StatusNotFound, "work item not found")
	case errors.Is(err, core.ErrUserNotFound):
		writeError(w, http.StatusBadRequest, "target user not found or inactive")
	case err != nil:
		h.Logger.Error("assign item failed", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to assign item")
	default:
		writeJSON(w, http.StatusOK, nil)
	}
}

// --- live presence / collision prevention ---

type heartbeatRequest struct {
	Mode string `json:"mode"` // "VIEWING" | "DRAFTING"
}

// Heartbeat handles POST /api/items/{id}/presence — called every ~10s by the
// client while a ticket is open, and once more with mode=VIEWING on unload.
func (h *Handlers) Heartbeat(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}

	var req heartbeatRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // best-effort; default to VIEWING below

	mode := presence.ModeViewing
	if req.Mode == string(presence.ModeDrafting) {
		mode = presence.ModeDrafting
	}

	u := actor(r)
	h.Presence.Heartbeat(id, u.ID, u.FullName, mode)
	writeJSON(w, http.StatusOK, map[string]any{"others": h.Presence.Others(id, u.ID)})
}

// ReleasePresence handles DELETE /api/items/{id}/presence — called on navigate-away.
func (h *Handlers) ReleasePresence(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}
	h.Presence.Release(id, actor(r).ID)
	w.WriteHeader(http.StatusNoContent)
}
