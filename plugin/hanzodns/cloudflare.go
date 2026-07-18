package hanzodns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// cfAPIBase is Cloudflare's API v4 origin. Overridable via CLOUDFLARE_API_BASE
// for tests (an httptest server) and CF-compatible endpoints; read at call time.
func cfAPIBase() string {
	if v := strings.TrimSpace(os.Getenv("CLOUDFLARE_API_BASE")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://api.cloudflare.com/client/v4"
}

var cfHTTPClient = &http.Client{Timeout: 20 * time.Second}

// cloudflareProvider implements Provider against the Cloudflare API v4 using a
// per-org scoped API token custodied in KMS. The token rides only the
// Authorization header — never a query parameter, error, or log line.
type cloudflareProvider struct {
	token string
	base  string
}

func newCloudflareProvider(token string) *cloudflareProvider {
	return &cloudflareProvider{token: token, base: cfAPIBase()}
}

func (p *cloudflareProvider) Type() string { return ProviderCloudflare }

// cfEnvelope is the shared Cloudflare API v4 response envelope.
type cfEnvelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

func (e cfEnvelope) err(status int) error {
	if e.Success {
		return nil
	}
	if len(e.Errors) > 0 {
		return fmt.Errorf("cloudflare API error (%d): [%d] %s", status, e.Errors[0].Code, e.Errors[0].Message)
	}
	return fmt.Errorf("cloudflare API error (status %d)", status)
}

// cfDo performs a Cloudflare API v4 call, JSON-encoding body, unwrapping the
// {success, errors, result} envelope, and decoding result into out. It fails
// closed: a transport error, an unsuccessful envelope, or a non-2xx status
// yields an error, and the error never contains the token.
func (p *cloudflareProvider) cfDo(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := cfHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare request failed")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var env cfEnvelope
	if len(data) > 0 {
		_ = json.Unmarshal(data, &env)
	}
	if err := env.err(resp.StatusCode); err != nil {
		return err
	}
	if out != nil {
		var wrap struct {
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(data, &wrap); err != nil {
			return fmt.Errorf("cloudflare: malformed response")
		}
		if len(wrap.Result) > 0 {
			if err := json.Unmarshal(wrap.Result, out); err != nil {
				return fmt.Errorf("cloudflare: malformed result")
			}
		}
	}
	return nil
}

type cfZone struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	NameServers []string `json:"name_servers"`
}

type cfRecord struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	TTL      uint32 `json:"ttl"`
	Proxied  bool   `json:"proxied"`
	Priority uint16 `json:"priority,omitempty"`
}

func (r cfRecord) toProvider() ProviderRecord {
	return ProviderRecord{
		ID: r.ID, Name: r.Name, Type: r.Type, Content: r.Content,
		TTL: r.TTL, Proxied: r.Proxied, Priority: r.Priority,
	}
}

func (p *cloudflareProvider) ListZones(ctx context.Context) ([]ProviderZone, error) {
	var zones []cfZone
	if err := p.cfDo(ctx, http.MethodGet, "/zones?per_page=50", nil, &zones); err != nil {
		return nil, err
	}
	out := make([]ProviderZone, 0, len(zones))
	for _, z := range zones {
		out = append(out, ProviderZone{ID: z.ID, Name: z.Name, Status: z.Status, NameServers: z.NameServers})
	}
	return out, nil
}

// ZoneIDForName resolves a zone NAME to its Cloudflare zone id via the CF
// name filter, so the Hanzo API's name-keyed routes address CF's id-keyed
// record endpoints.
func (p *cloudflareProvider) ZoneIDForName(ctx context.Context, name string) (string, error) {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	var zones []cfZone
	if err := p.cfDo(ctx, http.MethodGet, "/zones?name="+url.QueryEscape(name), nil, &zones); err != nil {
		return "", err
	}
	for _, z := range zones {
		if strings.EqualFold(strings.TrimSuffix(z.Name, "."), name) {
			return z.ID, nil
		}
	}
	return "", fmt.Errorf("zone %q not found in the connected cloudflare account", name)
}

func (p *cloudflareProvider) ListRecords(ctx context.Context, zoneID string) ([]ProviderRecord, error) {
	var recs []cfRecord
	if err := p.cfDo(ctx, http.MethodGet, "/zones/"+url.PathEscape(zoneID)+"/dns_records?per_page=100", nil, &recs); err != nil {
		return nil, err
	}
	out := make([]ProviderRecord, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.toProvider())
	}
	return out, nil
}

func (p *cloudflareProvider) CreateRecord(ctx context.Context, zoneID string, in RecordInput) (ProviderRecord, error) {
	var rec cfRecord
	if err := p.cfDo(ctx, http.MethodPost, "/zones/"+url.PathEscape(zoneID)+"/dns_records", cfRecordBody(in), &rec); err != nil {
		return ProviderRecord{}, err
	}
	return rec.toProvider(), nil
}

func (p *cloudflareProvider) UpdateRecord(ctx context.Context, zoneID, recordID string, in RecordInput) (ProviderRecord, error) {
	var rec cfRecord
	if err := p.cfDo(ctx, http.MethodPut, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recordID), cfRecordBody(in), &rec); err != nil {
		return ProviderRecord{}, err
	}
	return rec.toProvider(), nil
}

func (p *cloudflareProvider) DeleteRecord(ctx context.Context, zoneID, recordID string) error {
	return p.cfDo(ctx, http.MethodDelete, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(recordID), nil, nil)
}

// cfRecordBody builds the Cloudflare create/update payload. proxied (the orange
// cloud) is always sent; ttl and priority are included only when set.
func cfRecordBody(in RecordInput) map[string]any {
	m := map[string]any{
		"type":    in.Type,
		"name":    in.Name,
		"content": in.Content,
		"proxied": in.Proxied,
	}
	if in.TTL > 0 {
		m["ttl"] = in.TTL
	}
	if in.Priority > 0 {
		m["priority"] = in.Priority
	}
	return m
}
