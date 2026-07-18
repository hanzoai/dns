package hanzodns

import (
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

// kmsBaseURL is the Hanzo KMS REST origin. Defaults to the platform API and is
// overridable for non-prod / tests via HANZO_DNS_KMS_URL.
func kmsBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("HANZO_DNS_KMS_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://api.hanzo.ai"
}

var kmsHTTPClient = &http.Client{Timeout: 15 * time.Second}

// kmsSecret reads one org-scoped secret value from the Hanzo KMS REST API,
// relaying the CALLER's validated bearer (from ctx) as the Authorization so
// KMS's own org-scope guard authorizes the read. This is the least-privilege
// design: hanzodns holds no standing cross-tenant KMS credential, so a request
// can only reach the secrets of the org whose token it carried, and the KMS
// service — not this plugin — remains the single tenant-isolation boundary.
//
// subpath is the fixed secret coordinate under the org root, e.g.
// "integrations/cloudflare/api_token", producing
//
//	GET {base}/v1/kms/orgs/{org}/secrets/integrations/cloudflare/api_token?env=default
//
// The secret value is returned to the caller but is NEVER logged, wrapped into
// an error, or placed anywhere other than the returned string.
func kmsSecret(ctx context.Context, org, subpath string) (string, error) {
	bearer := BearerFromContext(ctx)
	if bearer == "" {
		return "", fmt.Errorf("no validated credential on request")
	}
	if org == "" {
		return "", fmt.Errorf("no org on request")
	}

	// org is a single validated path segment; subpath is a fixed in-code
	// coordinate (never client input), so only org is escaped.
	endpoint := kmsBaseURL() + "/v1/kms/orgs/" + url.PathEscape(org) + "/secrets/" + subpath + "?env=default"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)

	resp, err := kmsHTTPClient.Do(req)
	if err != nil {
		// Never wrap err: a transport error can echo the URL but never the
		// header. Keep the message credential-free regardless.
		return "", fmt.Errorf("kms request failed")
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		// fall through
	case http.StatusNotFound:
		return "", fmt.Errorf("cloudflare is not connected for this org")
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", fmt.Errorf("not authorized to read this org's cloudflare token")
	default:
		return "", fmt.Errorf("kms returned status %d", resp.StatusCode)
	}

	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("kms: malformed response")
	}
	if strings.TrimSpace(body.Value) == "" {
		return "", fmt.Errorf("cloudflare token is empty")
	}
	return body.Value, nil
}
