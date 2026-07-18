package hanzodns

import (
	"encoding/json"
	"net/http"
	"strings"
)

// SyncRequest is the top-level request body for POST /v1/dns/sync.
type SyncRequest struct {
	Zones []SyncZoneRequest `json:"zones"`
}

// handleSync handles POST /v1/dns/sync — bulk pushes zones + records.
func handleSync(w http.ResponseWriter, r *http.Request, store *Store) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is allowed")
		return
	}

	var body SyncRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	if len(body.Zones) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "at least one zone is required")
		return
	}

	// The owning org is the caller's validated `owner` claim, never the body, so
	// a sync can only create or replace the caller's own zones.
	org := reqOrg(r)
	results := make([]SyncResponse, 0, len(body.Zones))
	for _, zr := range body.Zones {
		if zr.Zone == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "zone name is required in each entry")
			return
		}
		resp, err := store.BulkSync(org, zr)
		if err != nil {
			// A zone owned by another org is refused as a conflict, not a 500.
			if strings.Contains(err.Error(), "already exists") {
				writeError(w, http.StatusConflict, "conflict", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		results = append(results, *resp)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"synced": results,
		"total":  len(results),
	})
}
