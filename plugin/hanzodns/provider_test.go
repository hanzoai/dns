package hanzodns

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const (
	testCFToken     = "cf-scoped-token-xyz"
	testUserBearer  = "user-org-bearer-abc"
	testOrg         = "acme"
	testCFZoneID    = "cfzone123"
	testCFZoneName  = "example.com"
	testCFRecordID  = "cfrec456"
	testKMSSubpath  = "integrations/cloudflare/api_token"
	testKMSFullPath = "/v1/kms/orgs/" + testOrg + "/secrets/" + testKMSSubpath
)

// fakeCloudflare is an httptest server that emulates the CF API v4 surface the
// connector uses, asserting the bearer token on every call.
func fakeCloudflare(t *testing.T) *httptest.Server {
	t.Helper()
	created := map[string]cfRecord{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+testCFToken {
			t.Errorf("cloudflare: bad Authorization %q", got)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		env := func(result any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": result})
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			if r.URL.Query().Get("name") != testCFZoneName {
				env([]cfZone{})
				return
			}
			env([]cfZone{{ID: testCFZoneID, Name: testCFZoneName, Status: "active", NameServers: []string{"ns1.cf.com"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/zones/"+testCFZoneID+"/dns_records":
			var body cfRecord
			_ = json.NewDecoder(r.Body).Decode(&body)
			body.ID = testCFRecordID
			created[body.ID] = body
			env(body)
		case r.Method == http.MethodGet && r.URL.Path == "/zones/"+testCFZoneID+"/dns_records":
			recs := make([]cfRecord, 0, len(created))
			for _, rec := range created {
				recs = append(recs, rec)
			}
			env(recs)
		case (r.Method == http.MethodPut || r.Method == http.MethodPatch) && r.URL.Path == "/zones/"+testCFZoneID+"/dns_records/"+testCFRecordID:
			var body cfRecord
			_ = json.NewDecoder(r.Body).Decode(&body)
			body.ID = testCFRecordID
			created[body.ID] = body
			env(body)
		case r.Method == http.MethodDelete && r.URL.Path == "/zones/"+testCFZoneID+"/dns_records/"+testCFRecordID:
			delete(created, testCFRecordID)
			env(map[string]string{"id": testCFRecordID})
		default:
			t.Errorf("cloudflare: unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// fakeKMS emulates the org-scoped KMS secret read, enforcing that the caller's
// bearer is relayed and that only the caller's own org path is readable.
func fakeKMS(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+testUserBearer {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Only the caller's own org path resolves; any other org is forbidden,
		// mirroring the KMS org-scope guard.
		if !strings.HasPrefix(r.URL.Path, "/v1/kms/orgs/"+testOrg+"/secrets/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.URL.Path != testKMSFullPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "api_token", "env": "default", "value": testCFToken})
	}))
}

// ctxWithPrincipal returns a context carrying the org + bearer the OIDC
// middleware would inject, so handler/dispatch tests exercise the provider path
// without the full JWKS dance.
func ctxWithPrincipal(org, bearer string) context.Context {
	ctx := context.WithValue(context.Background(), orgIDKey, org)
	return context.WithValue(ctx, bearerKey, bearer)
}

func TestKMSSecret_BearerRelay(t *testing.T) {
	kms := fakeKMS(t)
	defer kms.Close()
	t.Setenv("HANZO_DNS_KMS_URL", kms.URL)

	ctx := ctxWithPrincipal(testOrg, testUserBearer)
	tok, err := kmsSecret(ctx, testOrg, testKMSSubpath)
	if err != nil {
		t.Fatalf("kmsSecret: %v", err)
	}
	if tok != testCFToken {
		t.Fatalf("token = %q, want %q", tok, testCFToken)
	}

	// A caller with no bearer fails closed.
	if _, err := kmsSecret(context.Background(), testOrg, testKMSSubpath); err == nil {
		t.Fatal("expected failure with no bearer")
	}

	// A different org path is refused (the fake KMS mirrors the org-scope guard).
	if _, err := kmsSecret(ctx, "victim", testKMSSubpath); err == nil {
		t.Fatal("expected forbidden for foreign org")
	}
}

func TestCloudflareProvider_CRUD(t *testing.T) {
	cf := fakeCloudflare(t)
	defer cf.Close()
	t.Setenv("CLOUDFLARE_API_BASE", cf.URL)

	p := newCloudflareProvider(testCFToken)
	ctx := context.Background()

	zoneID, err := p.ZoneIDForName(ctx, testCFZoneName)
	if err != nil || zoneID != testCFZoneID {
		t.Fatalf("ZoneIDForName = %q, %v; want %q", zoneID, err, testCFZoneID)
	}

	rec, err := p.CreateRecord(ctx, zoneID, RecordInput{Name: "www." + testCFZoneName, Type: "A", Content: "1.2.3.4", TTL: 300, Proxied: true})
	if err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	if rec.ID != testCFRecordID || rec.Content != "1.2.3.4" || !rec.Proxied {
		t.Fatalf("created record = %+v", rec)
	}

	recs, err := p.ListRecords(ctx, zoneID)
	if err != nil || len(recs) != 1 || recs[0].Content != "1.2.3.4" {
		t.Fatalf("ListRecords = %+v, %v", recs, err)
	}

	if err := p.DeleteRecord(ctx, zoneID, testCFRecordID); err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}
	if recs, _ := p.ListRecords(ctx, zoneID); len(recs) != 0 {
		t.Fatalf("expected empty after delete, got %+v", recs)
	}
}

// TestDispatch_CloudflareZone drives the REST dispatch: a cloudflare-provider
// zone routes record CRUD through the connector (reading the token from KMS via
// the relayed bearer) and mirrors into the local store.
func TestDispatch_CloudflareZone(t *testing.T) {
	cf := fakeCloudflare(t)
	defer cf.Close()
	kms := fakeKMS(t)
	defer kms.Close()
	t.Setenv("CLOUDFLARE_API_BASE", cf.URL)
	t.Setenv("HANZO_DNS_KMS_URL", kms.URL)

	store := NewStore()
	if _, err := store.CreateZone(testOrg, testCFZoneName, ProviderCloudflare); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	router := apiRouter(store)

	// Create a record on the CF-provider zone.
	body := `{"name":"www.example.com","type":"A","content":"1.2.3.4","ttl":300,"proxied":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/dns/zones/"+testCFZoneName+"/records", strings.NewReader(body))
	req = req.WithContext(ctxWithPrincipal(testOrg, testUserBearer))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: status %d body %s", w.Code, w.Body.String())
	}
	var created Record
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.ID != testCFRecordID {
		t.Fatalf("created via dispatch = %+v", created)
	}

	// It was mirrored into the local store.
	if _, err := store.GetRecord(testOrg, testCFZoneName, testCFRecordID); err != nil {
		t.Fatalf("expected mirror in store: %v", err)
	}

	// List reads from the provider (source of truth).
	req = httptest.NewRequest(http.MethodGet, "/v1/dns/zones/"+testCFZoneName+"/records", nil)
	req = req.WithContext(ctxWithPrincipal(testOrg, testUserBearer))
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "1.2.3.4") {
		t.Fatalf("list: status %d body %s", w.Code, w.Body.String())
	}

	// Delete routes to the provider and drops the mirror.
	req = httptest.NewRequest(http.MethodDelete, "/v1/dns/zones/"+testCFZoneName+"/records/"+testCFRecordID, nil)
	req = req.WithContext(ctxWithPrincipal(testOrg, testUserBearer))
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: status %d body %s", w.Code, w.Body.String())
	}
	if _, err := store.GetRecord(testOrg, testCFZoneName, testCFRecordID); err == nil {
		t.Fatal("expected mirror dropped after delete")
	}
}

// TestDispatch_AuthoritativeZone confirms native zones stay first-class: records
// live in the store and resolve without any provider.
func TestDispatch_AuthoritativeZone(t *testing.T) {
	store := NewStore()
	if _, err := store.CreateZone(testOrg, "native.test", ProviderAuthoritative); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	router := apiRouter(store)

	body := `{"name":"api","type":"A","content":"10.0.0.1","ttl":60}`
	req := httptest.NewRequest(http.MethodPost, "/v1/dns/zones/native.test/records", strings.NewReader(body))
	req = req.WithContext(ctxWithPrincipal(testOrg, testUserBearer))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: status %d body %s", w.Code, w.Body.String())
	}

	// Served authoritatively by the store's DNS lookup.
	if got := store.Lookup("api.native.test.", "A"); len(got) != 1 || got[0].Content != "10.0.0.1" {
		t.Fatalf("Lookup = %+v", got)
	}
}

// TestCrossTenantIsolation is the regression for the store tenant-isolation
// HIGH: org B must never list, read, mutate, or delete org A's zone or records.
// Every cross-tenant access is a 404 (existence is not confirmed).
func TestCrossTenantIsolation(t *testing.T) {
	store := NewStore()
	router := apiRouter(store)
	const orgA, orgB = "orga", "orgb"

	do := func(org, method, path, body string) *httptest.ResponseRecorder {
		var r *http.Request
		if body != "" {
			r = httptest.NewRequest(method, path, strings.NewReader(body))
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		r = r.WithContext(ctxWithPrincipal(org, "bearer-"+org))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}

	if w := do(orgA, http.MethodPost, "/v1/dns/zones", `{"zone":"acme.example"}`); w.Code != http.StatusCreated {
		t.Fatalf("orgA create zone: %d %s", w.Code, w.Body.String())
	}
	if w := do(orgA, http.MethodPost, "/v1/dns/zones/acme.example/records", `{"name":"www","type":"A","content":"1.1.1.1"}`); w.Code != http.StatusCreated {
		t.Fatalf("orgA create record: %d %s", w.Code, w.Body.String())
	}
	var lr struct {
		Records []Record `json:"records"`
	}
	_ = json.Unmarshal(do(orgA, http.MethodGet, "/v1/dns/zones/acme.example/records", "").Body.Bytes(), &lr)
	if len(lr.Records) != 1 {
		t.Fatalf("orgA records: %+v", lr)
	}
	recID := lr.Records[0].ID

	// Org B sees no zones.
	var lz struct {
		Zones []Zone `json:"zones"`
		Total int    `json:"total"`
	}
	_ = json.Unmarshal(do(orgB, http.MethodGet, "/v1/dns/zones", "").Body.Bytes(), &lz)
	if lz.Total != 0 || len(lz.Zones) != 0 {
		t.Fatalf("orgB must see no zones, got %+v", lz)
	}

	// Every cross-tenant access to org A's zone/records is 404.
	cases := []struct{ method, path, body string }{
		{http.MethodGet, "/v1/dns/zones/acme.example", ""},
		{http.MethodDelete, "/v1/dns/zones/acme.example", ""},
		{http.MethodGet, "/v1/dns/zones/acme.example/records", ""},
		{http.MethodPost, "/v1/dns/zones/acme.example/records", `{"name":"evil","type":"A","content":"6.6.6.6"}`},
		{http.MethodGet, "/v1/dns/zones/acme.example/records/" + recID, ""},
		{http.MethodPut, "/v1/dns/zones/acme.example/records/" + recID, `{"content":"6.6.6.6"}`},
		{http.MethodDelete, "/v1/dns/zones/acme.example/records/" + recID, ""},
	}
	for _, c := range cases {
		if w := do(orgB, c.method, c.path, c.body); w.Code != http.StatusNotFound {
			t.Errorf("orgB %s %s: expected 404, got %d %s", c.method, c.path, w.Code, w.Body.String())
		}
	}

	// Org A's data is untouched after org B's attempts.
	w := do(orgA, http.MethodGet, "/v1/dns/zones/acme.example/records/"+recID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("orgA record should still exist: %d", w.Code)
	}
	var rec Record
	_ = json.Unmarshal(w.Body.Bytes(), &rec)
	if rec.Content != "1.1.1.1" {
		t.Fatalf("orgA record mutated by orgB: %+v", rec)
	}
}

// TestSyncIsOrgScoped confirms POST /v1/dns/sync is org-scoped: a caller cannot
// overwrite another org's zone, and the owner is the ctx principal, not the body.
func TestSyncIsOrgScoped(t *testing.T) {
	store := NewStore()
	sync := func(org, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/dns/sync", strings.NewReader(body)).
			WithContext(ctxWithPrincipal(org, "b-"+org))
		w := httptest.NewRecorder()
		handleSync(w, r, store)
		return w
	}

	if w := sync("orga", `{"zones":[{"zone":"shared.example","records":[{"name":"@","type":"A","content":"1.1.1.1"}]}]}`); w.Code != http.StatusOK {
		t.Fatalf("orgA sync: %d %s", w.Code, w.Body.String())
	}
	// Org B cannot overwrite org A's zone.
	if w := sync("orgb", `{"zones":[{"zone":"shared.example","records":[{"name":"@","type":"A","content":"6.6.6.6"}]}]}`); w.Code != http.StatusConflict {
		t.Fatalf("orgB sync of orgA's zone: expected 409, got %d %s", w.Code, w.Body.String())
	}
	// Org A's record is intact and org B still can't see the zone.
	recs, err := store.ListRecords("orga", "shared.example")
	if err != nil || len(recs) != 1 || recs[0].Content != "1.1.1.1" {
		t.Fatalf("orgA records after orgB attempt: %+v %v", recs, err)
	}
	if _, ok := store.ZoneProvider("orgb", "shared.example"); ok {
		t.Fatal("orgB must not see orgA's zone")
	}
}

func TestSnapshotDurability(t *testing.T) {
	dir := t.TempDir()

	// First "boot": create a native zone + record with snapshotting on.
	s1 := NewStore()
	if err := EnableSnapshot(s1, dir); err != nil {
		t.Fatalf("EnableSnapshot: %v", err)
	}
	if _, err := s1.CreateZone(testOrg, "persist.test", ProviderAuthoritative); err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	if _, err := s1.CreateRecord(testOrg, "persist.test", "@", TypeA, 300, "9.9.9.9", 0, false); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	if _, err := os.Stat(dir + "/" + snapshotFile); err != nil {
		t.Fatalf("snapshot not written: %v", err)
	}

	// Second "boot": a fresh store loads the snapshot and serves the record.
	s2 := NewStore()
	if err := EnableSnapshot(s2, dir); err != nil {
		t.Fatalf("EnableSnapshot reload: %v", err)
	}
	if got := s2.Lookup("persist.test.", "A"); len(got) != 1 || got[0].Content != "9.9.9.9" {
		t.Fatalf("after restart Lookup = %+v", got)
	}
	if p, ok := s2.ZoneProvider(testOrg, "persist.test"); !ok || p != ProviderAuthoritative {
		t.Fatalf("provider after restart = %q, %v", p, ok)
	}
}
