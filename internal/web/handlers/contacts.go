package handlers

import "net/http"

// ListOrganizations handles GET /api/organizations?q= — a typeahead search
// backing the queue's organization filter and the "Promote to Contact"
// modal's optional organization field. No dedicated contacts/organizations
// management UI exists yet (out of scope for Round 3's telephony/UI ask);
// this exposes just enough of core.ContactService for those two lookups.
func (h *Handlers) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	orgs, err := h.Contacts.ListOrganizations(r.Context(), r.URL.Query().Get("q"), 20, 0)
	if err != nil {
		h.Logger.Error("list organizations failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list organizations")
		return
	}
	writeJSON(w, http.StatusOK, orgs)
}
