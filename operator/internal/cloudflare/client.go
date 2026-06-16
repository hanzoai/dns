// Package cloudflare is a minimal Cloudflare API v4 client covering exactly the
// surface the operator needs: DNS record CRUD and Pages project management.
// Auth is a scoped API token (Bearer) — never the Global API Key.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// DefaultBaseURL is the Cloudflare API v4 root. Overridable for tests.
const DefaultBaseURL = "https://api.cloudflare.com/client/v4"

// Client talks to the Cloudflare API with a scoped Bearer token.
type Client struct {
	Token   string
	BaseURL string
	HTTP    *http.Client
}

// New returns a Client with sensible defaults.
func New(token string) *Client {
	return &Client{Token: token, BaseURL: DefaultBaseURL, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Record is a Cloudflare DNS record (the fields the operator manages).
type Record struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl,omitempty"`
	Proxied  bool   `json:"proxied"`
	Priority *int   `json:"priority,omitempty"`
}

// PagesProject is a Cloudflare Pages project (the fields the operator manages).
type PagesProject struct {
	Name             string `json:"name"`
	Subdomain        string `json:"subdomain,omitempty"`
	ProductionBranch string `json:"production_branch,omitempty"`
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e apiError) Error() string { return fmt.Sprintf("cloudflare: %d %s", e.Code, e.Message) }

type apiResponse struct {
	Success bool            `json:"success"`
	Errors  []apiError      `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

// do issues one API call and unmarshals result into out (which may be nil).
// It is the single HTTP path for the whole client — DRY by construction.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var ar apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return fmt.Errorf("cloudflare: decode %s %s: %w", method, path, err)
	}
	if !ar.Success {
		if len(ar.Errors) > 0 {
			return ar.Errors[0]
		}
		return fmt.Errorf("cloudflare: %s %s failed (HTTP %d)", method, path, resp.StatusCode)
	}
	if out != nil && len(ar.Result) > 0 {
		return json.Unmarshal(ar.Result, out)
	}
	return nil
}

// ---- DNS records -----------------------------------------------------------

// FindRecord returns the record matching name+type in the zone, or nil if absent.
func (c *Client) FindRecord(ctx context.Context, zoneID, name, recType string) (*Record, error) {
	q := url.Values{"name": {name}, "type": {recType}}
	var recs []Record
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/zones/%s/dns_records?%s", zoneID, q.Encode()), nil, &recs); err != nil {
		return nil, err
	}
	if len(recs) == 0 {
		return nil, nil
	}
	return &recs[0], nil
}

// CreateRecord creates a record and returns its Cloudflare ID.
func (c *Client) CreateRecord(ctx context.Context, zoneID string, r Record) (string, error) {
	var out Record
	if err := c.do(ctx, http.MethodPost, "/zones/"+zoneID+"/dns_records", r, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// UpdateRecord overwrites an existing record by ID.
func (c *Client) UpdateRecord(ctx context.Context, zoneID, id string, r Record) error {
	return c.do(ctx, http.MethodPut, "/zones/"+zoneID+"/dns_records/"+id, r, nil)
}

// DeleteRecord removes a record by ID. A 404/already-gone is not an error.
func (c *Client) DeleteRecord(ctx context.Context, zoneID, id string) error {
	err := c.do(ctx, http.MethodDelete, "/zones/"+zoneID+"/dns_records/"+id, nil, nil)
	if ae, ok := err.(apiError); ok && ae.Code == 81044 { // record does not exist
		return nil
	}
	return err
}

// ---- Pages -----------------------------------------------------------------

// GetPagesProject returns the project, or nil if it does not exist.
func (c *Client) GetPagesProject(ctx context.Context, accountID, name string) (*PagesProject, error) {
	var p PagesProject
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/accounts/%s/pages/projects/%s", accountID, name), nil, &p)
	if ae, ok := err.(apiError); ok && ae.Code == 8000007 { // project not found
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CreatePagesProject creates a Pages project and returns it (with its pages.dev subdomain).
func (c *Client) CreatePagesProject(ctx context.Context, accountID string, p PagesProject) (*PagesProject, error) {
	if p.ProductionBranch == "" {
		p.ProductionBranch = "main"
	}
	var out PagesProject
	if err := c.do(ctx, http.MethodPost, "/accounts/"+accountID+"/pages/projects", p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AddPagesDomain attaches a custom domain to a Pages project. Already-attached is not an error.
func (c *Client) AddPagesDomain(ctx context.Context, accountID, project, domain string) error {
	err := c.do(ctx, http.MethodPost,
		fmt.Sprintf("/accounts/%s/pages/projects/%s/domains", accountID, project),
		map[string]string{"name": domain}, nil)
	if ae, ok := err.(apiError); ok && ae.Code == 8000038 { // domain already exists
		return nil
	}
	return err
}
