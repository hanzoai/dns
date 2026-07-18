package hanzodns

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

// errZoneNotFound is the sentinel the backend resolver returns for an unknown
// zone, mapped to a 404 by the handlers.
var errZoneNotFound = errors.New("zone not found")

// apiError is the standard error response body.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, apiError{Code: code, Message: msg})
}

// authMiddleware validates the Bearer token against HANZO_DNS_API_KEY.
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := os.Getenv("HANZO_DNS_API_KEY")
		if key == "" {
			// No key configured -- allow all requests (development mode).
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing Authorization header")
			return
		}

		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") || parts[1] != key {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid bearer token")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// registerRoutes wires up all REST API handlers on the given mux.
func registerRoutes(mux *http.ServeMux, store *Store) {
	// Health -- no auth required.
	mux.HandleFunc("GET /v1/dns/health", handleHealth)

	// All other routes require auth.
	authed := authChain(apiRouter(store))
	mux.Handle("/v1/dns/zones", authed)
	mux.Handle("/v1/dns/zones/", authed)

	// Bulk sync endpoint -- requires auth.
	mux.Handle("/v1/dns/sync", authChain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleSync(w, r, store)
	})))
}

// authChain selects the API's ONE auth boundary by configuration: when an OIDC
// issuer is set it validates the caller's IAM JWT (and makes org + bearer
// available for the KMS-backed provider path); otherwise it falls back to the
// static shared-key check. This keeps provider dispatch (which needs the org
// and the caller's bearer) available in production while preserving the simple
// key mode for standalone deployments.
func authChain(next http.Handler) http.Handler {
	if os.Getenv("HANZO_DNS_OIDC_ISSUER") != "" {
		return oidcMiddleware(next)
	}
	return authMiddleware(next)
}

// zoneBackend resolves how a zone's records are served. It returns (nil, "", nil)
// for an authoritative zone (the local store is the backend); for a
// provider-backed zone it builds the connector from the org's KMS-sealed token
// and resolves the provider's zone id. errZoneNotFound signals an unknown zone.
func zoneBackend(r *http.Request, store *Store, zoneName string) (Provider, string, error) {
	kind, ok := store.ZoneProvider(zoneName)
	if !ok {
		return nil, "", errZoneNotFound
	}
	if kind == ProviderAuthoritative {
		return nil, "", nil
	}
	ctx := r.Context()
	prov, err := providerFor(ctx, OrgIDFromContext(ctx), kind)
	if err != nil {
		return nil, "", err
	}
	zoneID, err := prov.ZoneIDForName(ctx, zoneName)
	if err != nil {
		return nil, "", err
	}
	return prov, zoneID, nil
}

// toRecord maps a provider record into the store's Record shape for a uniform
// API response across authoritative and provider-backed zones.
func toRecord(pr ProviderRecord) Record {
	now := time.Now().UTC()
	return Record{
		ID: pr.ID, Name: pr.Name, Type: RecordType(pr.Type), TTL: pr.TTL,
		Content: pr.Content, Priority: pr.Priority, Proxied: pr.Proxied,
		CreatedAt: now, UpdatedAt: now,
	}
}

// writeBackendError maps a backend-resolution error to an HTTP response.
func writeBackendError(w http.ResponseWriter, err error) {
	if errors.Is(err, errZoneNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "zone not found")
		return
	}
	writeError(w, http.StatusBadGateway, "provider_error", err.Error())
}

// apiRouter returns a handler that dispatches /v1/dns/zones* requests.
func apiRouter(store *Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/v1/dns")
		path = strings.TrimSuffix(path, "/")
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")

		// /zones
		if len(parts) == 1 && parts[0] == "zones" {
			switch r.Method {
			case http.MethodGet:
				handleListZones(w, r, store)
			case http.MethodPost:
				handleCreateZone(w, r, store)
			default:
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			}
			return
		}

		// /zones/{zone}
		if len(parts) == 2 && parts[0] == "zones" {
			zoneName := parts[1]
			switch r.Method {
			case http.MethodGet:
				handleGetZone(w, r, store, zoneName)
			case http.MethodDelete:
				handleDeleteZone(w, r, store, zoneName)
			default:
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			}
			return
		}

		// /zones/{zone}/records
		if len(parts) == 3 && parts[0] == "zones" && parts[2] == "records" {
			zoneName := parts[1]
			switch r.Method {
			case http.MethodGet:
				handleListRecords(w, r, store, zoneName)
			case http.MethodPost:
				handleCreateRecord(w, r, store, zoneName)
			default:
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			}
			return
		}

		// /zones/{zone}/records/{id}
		if len(parts) == 4 && parts[0] == "zones" && parts[2] == "records" {
			zoneName := parts[1]
			recordID := parts[3]
			switch r.Method {
			case http.MethodGet:
				handleGetRecord(w, r, store, zoneName, recordID)
			case http.MethodPatch:
				handleUpdateRecord(w, r, store, zoneName, recordID)
			case http.MethodPut:
				handleUpdateRecord(w, r, store, zoneName, recordID)
			case http.MethodDelete:
				handleDeleteRecord(w, r, store, zoneName, recordID)
			default:
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			}
			return
		}

		writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
	})
}

// --- Handlers ---

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleListZones(w http.ResponseWriter, r *http.Request, store *Store) {
	zones := store.ListZones()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"zones": zones,
		"total": len(zones),
	})
}

func handleGetZone(w http.ResponseWriter, r *http.Request, store *Store, zoneName string) {
	z := store.GetZone(zoneName)
	if z == nil {
		writeError(w, http.StatusNotFound, "not_found", "zone not found")
		return
	}
	writeJSON(w, http.StatusOK, z)
}

func handleCreateZone(w http.ResponseWriter, r *http.Request, store *Store) {
	var body struct {
		Zone     string `json:"zone"`
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if body.Zone == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "zone name is required")
		return
	}
	provider, err := normProvider(body.Provider)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	z, err := store.CreateZone(body.Zone, provider)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeError(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, z)
}

func handleDeleteZone(w http.ResponseWriter, r *http.Request, store *Store, zoneName string) {
	if err := store.DeleteZone(zoneName); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{})
}

func handleListRecords(w http.ResponseWriter, r *http.Request, store *Store, zoneName string) {
	prov, zoneID, err := zoneBackend(r, store, zoneName)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	if prov != nil {
		// Provider-backed zone: the provider is the source of truth.
		prs, err := prov.ListRecords(r.Context(), zoneID)
		if err != nil {
			writeError(w, http.StatusBadGateway, "provider_error", err.Error())
			return
		}
		records := make([]Record, 0, len(prs))
		for _, pr := range prs {
			records = append(records, toRecord(pr))
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"records": records, "total": len(records)})
		return
	}

	records, err := store.ListRecords(zoneName)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"records": records,
		"total":   len(records),
	})
}

func handleGetRecord(w http.ResponseWriter, r *http.Request, store *Store, zoneName, recordID string) {
	rec, err := store.GetRecord(zoneName, recordID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func handleCreateRecord(w http.ResponseWriter, r *http.Request, store *Store, zoneName string) {
	var body struct {
		Name     string     `json:"name"`
		Type     RecordType `json:"type"`
		TTL      uint32     `json:"ttl"`
		Content  string     `json:"content"`
		Priority uint16     `json:"priority"`
		Proxied  bool       `json:"proxied"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "name is required")
		return
	}
	if body.Type == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "type is required")
		return
	}
	if body.Content == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "content is required")
		return
	}
	if body.TTL == 0 {
		body.TTL = 300
	}

	prov, zoneID, err := zoneBackend(r, store, zoneName)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	if prov != nil {
		// Provider-backed zone: create in the provider (source of truth), then
		// mirror into the local store so CoreDNS can serve it and the local API
		// stays consistent.
		pr, err := prov.CreateRecord(r.Context(), zoneID, RecordInput{
			Name: body.Name, Type: string(body.Type), Content: body.Content,
			TTL: body.TTL, Proxied: body.Proxied, Priority: body.Priority,
		})
		if err != nil {
			writeError(w, http.StatusBadGateway, "provider_error", err.Error())
			return
		}
		_ = store.PutMirror(zoneName, pr)
		writeJSON(w, http.StatusCreated, toRecord(pr))
		return
	}

	rec, err := store.CreateRecord(zoneName, body.Name, body.Type, body.TTL, body.Content, body.Priority, body.Proxied)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rec)
}

func handleUpdateRecord(w http.ResponseWriter, r *http.Request, store *Store, zoneName, recordID string) {
	var patch RecordPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	prov, zoneID, err := zoneBackend(r, store, zoneName)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	if prov != nil {
		// Merge the patch onto the mirrored record to form the full desired
		// state, then replace it in the provider and re-mirror the result.
		cur, gerr := store.GetRecord(zoneName, recordID)
		if gerr != nil {
			writeError(w, http.StatusNotFound, "not_found", "record not found in local index; list the zone to refresh")
			return
		}
		pr, uerr := prov.UpdateRecord(r.Context(), zoneID, recordID, applyPatch(cur, patch))
		if uerr != nil {
			writeError(w, http.StatusBadGateway, "provider_error", uerr.Error())
			return
		}
		_ = store.PutMirror(zoneName, pr)
		writeJSON(w, http.StatusOK, toRecord(pr))
		return
	}

	rec, err := store.UpdateRecord(zoneName, recordID, patch)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// applyPatch merges a partial record patch onto the current record, yielding the
// full desired state a provider PUT requires.
func applyPatch(cur *Record, patch RecordPatch) RecordInput {
	in := RecordInput{
		Name: cur.Name, Type: string(cur.Type), Content: cur.Content,
		TTL: cur.TTL, Proxied: cur.Proxied, Priority: cur.Priority,
	}
	if patch.Name != nil {
		in.Name = *patch.Name
	}
	if patch.Type != nil {
		in.Type = string(*patch.Type)
	}
	if patch.TTL != nil {
		in.TTL = *patch.TTL
	}
	if patch.Content != nil {
		in.Content = *patch.Content
	}
	if patch.Priority != nil {
		in.Priority = *patch.Priority
	}
	if patch.Proxied != nil {
		in.Proxied = *patch.Proxied
	}
	return in
}

func handleDeleteRecord(w http.ResponseWriter, r *http.Request, store *Store, zoneName, recordID string) {
	prov, zoneID, err := zoneBackend(r, store, zoneName)
	if err != nil {
		writeBackendError(w, err)
		return
	}
	if prov != nil {
		if derr := prov.DeleteRecord(r.Context(), zoneID, recordID); derr != nil {
			writeError(w, http.StatusBadGateway, "provider_error", derr.Error())
			return
		}
		store.DropMirror(zoneName, recordID)
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}

	if err := store.DeleteRecord(zoneName, recordID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{})
}
