package handlers

import (
	"errors"
	"net/http"

	"github.com/project-chassis/chassis/internal/core"
)

// LinkAsset handles POST /api/items/{id}/assets/{assetID}.
func (h *Handlers) LinkAsset(w http.ResponseWriter, r *http.Request) {
	itemID, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}
	assetID, err := pathInt64(r, "assetID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid asset id")
		return
	}

	err = h.Service.LinkAsset(r.Context(), itemID, assetID, actor(r).ID)
	switch {
	case errors.Is(err, core.ErrItemNotFound):
		writeError(w, http.StatusNotFound, "work item not found")
	case err != nil:
		h.Logger.Error("link asset failed", "item_id", itemID, "asset_id", assetID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to link asset")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// UnlinkAsset handles DELETE /api/items/{id}/assets/{assetID}.
func (h *Handlers) UnlinkAsset(w http.ResponseWriter, r *http.Request) {
	itemID, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}
	assetID, err := pathInt64(r, "assetID")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid asset id")
		return
	}

	if err := h.Service.UnlinkAsset(r.Context(), itemID, assetID, actor(r).ID); err != nil {
		h.Logger.Error("unlink asset failed", "item_id", itemID, "asset_id", assetID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to unlink asset")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
