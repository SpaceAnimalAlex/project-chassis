package handlers

import "net/http"

// Health handles GET /api/health — a trivial liveness probe. It deliberately
// does not touch the database: if the process is up and serving, that's the
// answer. DB-level health is checked at startup (migrations) and by the mail
// sync worker's own logging, not re-derived here.
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListImplements handles GET /api/implements — the registered domain
// Implements (IT, Edu, Civic, MRO), used by the UI to drive domain-specific
// labels and asset-type pickers without hardcoding them client-side.
func (h *Handlers) ListImplements(w http.ResponseWriter, r *http.Request) {
	impls := h.Registry.List()
	out := make([]map[string]any, 0, len(impls))
	for _, impl := range impls {
		out = append(out, map[string]any{
			"domain":               impl.Domain(),
			"display_name":         impl.DisplayName(),
			"item_code_prefix":     impl.ItemCodePrefix(),
			"supported_asset_types": impl.SupportedAssetTypes(),
		})
	}
	writeJSON(w, http.StatusOK, out)
}
