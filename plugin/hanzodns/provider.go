package hanzodns

import (
	"context"
	"fmt"
)

// A zone is served by exactly one backend, named by its provider. Authoritative
// zones live in this CoreDNS store and are answered locally; provider zones are
// delegated to a third-party control plane (Cloudflare today) via the org's
// KMS-sealed credential. Both are first-class: an org may run some zones on
// Hanzo's own nameservers and point others at a connected Cloudflare account.
const (
	ProviderAuthoritative = "authoritative"
	ProviderCloudflare    = "cloudflare"
)

// validProviders is the set of provider names a zone may declare.
var validProviders = map[string]bool{
	ProviderAuthoritative: true,
	ProviderCloudflare:    true,
}

// ProviderZone is a zone as reported by an upstream provider.
type ProviderZone struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	NameServers []string `json:"name_servers"`
}

// ProviderRecord is a record as reported by an upstream provider.
type ProviderRecord struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	TTL      uint32 `json:"ttl"`
	Proxied  bool   `json:"proxied"`
	Priority uint16 `json:"priority,omitempty"`
}

// RecordInput is the payload for creating or updating a provider record.
type RecordInput struct {
	Name     string
	Type     string
	Content  string
	TTL      uint32
	Proxied  bool
	Priority uint16
}

// Provider is the connector contract for a third-party DNS control plane. It
// mirrors the platform's dns-provider.ts interface: an implementation holds a
// per-org credential and maps the canonical operations onto the provider API.
// Records are addressed by the provider's own zone/record ids; ZoneIDForName
// bridges the Hanzo API (keyed by zone NAME) to that id space.
type Provider interface {
	Type() string
	ListZones(ctx context.Context) ([]ProviderZone, error)
	ZoneIDForName(ctx context.Context, name string) (string, error)
	ListRecords(ctx context.Context, zoneID string) ([]ProviderRecord, error)
	CreateRecord(ctx context.Context, zoneID string, in RecordInput) (ProviderRecord, error)
	UpdateRecord(ctx context.Context, zoneID, recordID string, in RecordInput) (ProviderRecord, error)
	DeleteRecord(ctx context.Context, zoneID, recordID string) error
}

// providerFor builds the Provider for (org, kind).
//
// For an authoritative zone it returns (nil, nil): the local store is the
// backend, so there is nothing to construct. For "cloudflare" it reads the
// org's scoped API token from KMS using the caller's own bearer (relayed from
// ctx by kmsSecret) and returns a Cloudflare connector bound to that token. The
// token is auth-method-agnostic: whether the org connected Cloudflare via an
// API key or OAuth, both seal the token to the same KMS coordinate, so this
// path just reads it.
func providerFor(ctx context.Context, org, kind string) (Provider, error) {
	switch kind {
	case "", ProviderAuthoritative:
		return nil, nil
	case ProviderCloudflare:
		token, err := kmsSecret(ctx, org, "integrations/cloudflare/api_token")
		if err != nil {
			return nil, err
		}
		return newCloudflareProvider(token), nil
	default:
		return nil, fmt.Errorf("unknown provider %q", kind)
	}
}

// normProvider validates and normalizes a requested provider name, defaulting
// to authoritative when unset.
func normProvider(p string) (string, error) {
	if p == "" {
		return ProviderAuthoritative, nil
	}
	if !validProviders[p] {
		return "", fmt.Errorf("unknown provider %q", p)
	}
	return p, nil
}
