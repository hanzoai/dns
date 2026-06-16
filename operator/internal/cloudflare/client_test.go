package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestClient returns a Client pointed at a mock server with the given handler.
func newTestClient(h http.HandlerFunc) (*Client, *httptest.Server) {
	srv := httptest.NewServer(h)
	c := New("test-token")
	c.BaseURL = srv.URL
	return c, srv
}

func wrap(result any) []byte {
	b, _ := json.Marshal(map[string]any{"success": true, "errors": []any{}, "result": result})
	return b
}

func TestCreateRecord(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/zones/z1/dns_records" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("auth = %q", got)
		}
		var in Record
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Name != "www.example.com" || in.Type != "A" || in.Content != "1.2.3.4" {
			t.Errorf("body = %+v", in)
		}
		in.ID = "rec123"
		_, _ = w.Write(wrap(in))
	})
	defer srv.Close()

	id, err := c.CreateRecord(context.Background(), "z1", Record{Type: "A", Name: "www.example.com", Content: "1.2.3.4", Proxied: true})
	if err != nil {
		t.Fatal(err)
	}
	if id != "rec123" {
		t.Errorf("id = %q, want rec123", id)
	}
}

func TestFindRecord(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != "www.example.com" || r.URL.Query().Get("type") != "CNAME" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write(wrap([]Record{{ID: "rec9", Type: "CNAME", Name: "www.example.com", Content: "target.pages.dev"}}))
	})
	defer srv.Close()

	got, err := c.FindRecord(context.Background(), "z1", "www.example.com", "CNAME")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != "rec9" || got.Content != "target.pages.dev" {
		t.Errorf("got = %+v", got)
	}
}

func TestFindRecordAbsent(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(wrap([]Record{}))
	})
	defer srv.Close()

	got, err := c.FindRecord(context.Background(), "z1", "missing.example.com", "A")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("want nil, got %+v", got)
	}
}

func TestDeleteRecordIdempotent(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		// Simulate "record does not exist" — must be swallowed.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"errors":  []apiError{{Code: 81044, Message: "Record does not exist."}},
		})
	})
	defer srv.Close()

	if err := c.DeleteRecord(context.Background(), "z1", "gone"); err != nil {
		t.Errorf("delete of absent record should be nil, got %v", err)
	}
}

func TestAPIErrorSurfaced(t *testing.T) {
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"errors":  []apiError{{Code: 10000, Message: "Authentication error"}},
		})
	})
	defer srv.Close()

	_, err := c.CreateRecord(context.Background(), "z1", Record{Type: "A", Name: "x", Content: "1.1.1.1"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPagesProjectLifecycle(t *testing.T) {
	created := false
	c, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && !created:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": false, "errors": []apiError{{Code: 8000007, Message: "not found"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/accounts/acc1/pages/projects":
			created = true
			var in PagesProject
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.ProductionBranch != "main" {
				t.Errorf("default branch = %q", in.ProductionBranch)
			}
			in.Subdomain = in.Name + ".pages.dev"
			_, _ = w.Write(wrap(in))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	defer srv.Close()
	ctx := context.Background()

	p, err := c.GetPagesProject(ctx, "acc1", "site")
	if err != nil || p != nil {
		t.Fatalf("get absent: p=%v err=%v", p, err)
	}
	got, err := c.CreatePagesProject(ctx, "acc1", PagesProject{Name: "site"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Subdomain != "site.pages.dev" {
		t.Errorf("subdomain = %q", got.Subdomain)
	}
}
