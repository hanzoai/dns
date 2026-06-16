package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	dnsv1alpha1 "github.com/hanzoai/dns-operator/api/v1alpha1"
	"github.com/hanzoai/dns-operator/internal/cloudflare"
)

func scheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := dnsv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func ok(w http.ResponseWriter, result any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": result})
}

func TestCloudflareReconcileCreatesAndRecordsID(t *testing.T) {
	var createCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet: // FindRecord → none exists yet
			ok(w, []cloudflare.Record{})
		case http.MethodPost: // CreateRecord
			createCalls++
			var in cloudflare.Record
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.Name != "www.example.com" || in.Content != "1.2.3.4" {
				t.Errorf("create body = %+v", in)
			}
			in.ID = "cf-rec-1"
			ok(w, in)
		default:
			t.Errorf("unexpected %s", r.Method)
		}
	}))
	defer srv.Close()
	cf := cloudflare.New("t")
	cf.BaseURL = srv.URL

	s := scheme(t)
	zone := &dnsv1alpha1.DnsZone{}
	zone.Name, zone.Namespace = "z", "ns"
	zone.Spec.Zone, zone.Spec.CloudflareZoneID = "example.com", "cf-zone-1"
	rec := &dnsv1alpha1.DnsRecord{}
	rec.Name, rec.Namespace = "r", "ns"
	rec.Spec = dnsv1alpha1.DnsRecordSpec{ZoneRef: "z", Name: "www", Type: "A", Content: "1.2.3.4", SyncToCloudflare: true}

	cl := fake.NewClientBuilder().WithScheme(s).
		WithObjects(zone, rec).WithStatusSubresource(rec).Build()
	r := &CloudflareReconciler{Client: cl, Scheme: s, CF: cf}

	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "r", Namespace: "ns"}}); err != nil {
		t.Fatal(err)
	}

	var got dnsv1alpha1.DnsRecord
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "r", Namespace: "ns"}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.CloudflareRecordID != "cf-rec-1" {
		t.Errorf("CloudflareRecordID = %q, want cf-rec-1", got.Status.CloudflareRecordID)
	}
	if got.Status.Phase != "Active" {
		t.Errorf("Phase = %q, want Active", got.Status.Phase)
	}
	if createCalls != 1 {
		t.Errorf("create calls = %d, want 1", createCalls)
	}
}

func TestFqdn(t *testing.T) {
	cases := []struct{ name, zone, want string }{
		{"@", "hanzo.bot", "hanzo.bot"},
		{"www", "hanzo.ai", "www.hanzo.ai"},
		{"hanzo.bot", "hanzo.bot", "hanzo.bot"},
		{"www.hanzo.ai", "hanzo.ai", "www.hanzo.ai"},
	}
	for _, c := range cases {
		if got := fqdn(c.name, c.zone); got != c.want {
			t.Errorf("fqdn(%q,%q) = %q, want %q", c.name, c.zone, got, c.want)
		}
	}
}

func TestPagesReconcileCreatesProjectAndChildRecord(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet: // GetPagesProject → not found
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false,
				"errors": []map[string]any{{"code": 8000007, "message": "not found"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/accounts/acc/pages/projects":
			ok(w, cloudflare.PagesProject{Name: "bot-site", Subdomain: "bot-site.pages.dev"})
		case r.Method == http.MethodPost: // AddPagesDomain
			ok(w, map[string]any{})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	cf := cloudflare.New("t")
	cf.BaseURL = srv.URL

	s := scheme(t)
	pp := &dnsv1alpha1.PagesProject{}
	pp.Name, pp.Namespace = "bot-site", "ns"
	pp.Spec = dnsv1alpha1.PagesProjectSpec{AccountID: "acc", CustomDomain: "hanzo.bot", ZoneRef: "z"}

	cl := fake.NewClientBuilder().WithScheme(s).
		WithObjects(pp).WithStatusSubresource(pp).Build()
	r := &PagesProjectReconciler{Client: cl, Scheme: s, CF: cf}

	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "bot-site", Namespace: "ns"}}); err != nil {
		t.Fatal(err)
	}

	var got dnsv1alpha1.PagesProject
	_ = cl.Get(context.Background(), types.NamespacedName{Name: "bot-site", Namespace: "ns"}, &got)
	if got.Status.Phase != "Ready" || got.Status.Subdomain != "bot-site.pages.dev" {
		t.Errorf("status = %+v", got.Status)
	}

	// The composed child CNAME must exist and point at the pages.dev host.
	var child dnsv1alpha1.DnsRecord
	if err := cl.Get(context.Background(), types.NamespacedName{Name: "bot-site-domain", Namespace: "ns"}, &child); err != nil {
		t.Fatalf("child DnsRecord not created: %v", err)
	}
	if child.Spec.Type != "CNAME" || child.Spec.Content != "bot-site.pages.dev" ||
		child.Spec.Name != "hanzo.bot" || !child.Spec.SyncToCloudflare {
		t.Errorf("child record = %+v", child.Spec)
	}
	if len(child.OwnerReferences) != 1 || child.OwnerReferences[0].Name != "bot-site" {
		t.Errorf("child ownerRefs = %+v", child.OwnerReferences)
	}
}
