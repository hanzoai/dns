# hanzodns

## Name

*hanzodns* - REST API for dynamic DNS zone and record management.

## Description

The hanzodns plugin exposes a REST API that allows creating, reading, updating, and deleting DNS zones and records at runtime. Records added through the API are immediately available for DNS resolution by CoreDNS.

The plugin runs a side-car HTTP server (default `:8443`) alongside CoreDNS and stores records in-memory. The store is designed to be swappable for a persistent backend (e.g., PostgreSQL) in the future.

## Syntax

```
hanzodns [ADDR]
```

- **ADDR** - the address to listen on for the HTTP API (default `:8443`).

## Authentication

The API has one auth boundary, selected by configuration, and **fails closed**:

- **OIDC (production, multi-tenant).** Set `HANZO_DNS_OIDC_ISSUER` (e.g. `https://hanzo.id`) to validate the caller's IAM JWT on every endpoint (except `/v1/dns/health`). The middleware requires a signed, unexpired token, extracts the org (the `owner` claim), and keeps the caller's bearer for the provider path. Set `HANZO_DNS_OIDC_AUDIENCE` to a comma-separated allowlist to also enforce the audience (reject tokens minted for other services) — required in production.
- **Static key (standalone, single-tenant).** With no issuer, `HANZO_DNS_API_KEY` requires a shared Bearer token.
- **Denied.** With neither configured the API returns 503 — the anonymous "allow all" is reachable ONLY behind an explicit `HANZO_DNS_DEV_INSECURE=1` opt-in, never by default.

```
Authorization: Bearer <token>
```

### Tenant isolation

Every zone is owned by the org that created it (the validated `owner` claim), and org is the single isolation key enforced inside the store, not just at the edge: a caller only ever lists, reads, or mutates its own org's zones and records. A request for another org's zone or record is `404` (existence is never confirmed cross-tenant), and `/v1/dns/sync` can only create or replace the caller's own zones. Multi-tenant deployments therefore require OIDC (the static-key mode is single-tenant).

## Providers

A zone is served by one backend, named by its `provider`:

- **`authoritative`** (default) — records live in this CoreDNS store and are answered locally. This is Hanzo's own nameservers; fully first-class.
- **`cloudflare`** — records are managed in the org's connected Cloudflare account. The plugin reads the org's scoped API token from Hanzo KMS (path `/orgs/{org}/integrations/cloudflare/api_token`) by relaying the caller's own validated bearer to the KMS REST API (`{HANZO_DNS_KMS_URL}/v1/kms/orgs/{org}/secrets/...`, default `https://api.hanzo.ai`). The plugin holds no standing cross-tenant credential — KMS enforces org isolation, and the token is never logged. Provider-backed records are also mirrored into the local store so CoreDNS can serve the zone and listings stay consistent.

Create a Cloudflare-backed zone by passing `provider` on zone creation:

```bash
curl -X POST https://dns.hanzo.ai/v1/dns/zones \
  -H "Authorization: Bearer $ORG_JWT" \
  -d '{"zone": "example.com", "provider": "cloudflare"}'
```

## Durability

Authoritative zones are held in memory. Set `HANZO_DNS_STATE_DIR` to a writable directory to make them durable: the store loads its last snapshot on startup and rewrites it (atomically) after every mutation, so a restart preserves native zones. Unset keeps the pure in-memory store. Cross-replica consistency and reschedule durability are provided by the platform re-pushing authoritative zones via `POST /v1/dns/sync` (the CRD `150-dns-records` reconcile).

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/v1/dns/health` | Health check (no auth) |
| GET | `/v1/dns/zones` | List all zones |
| POST | `/v1/dns/zones` | Create a zone |
| GET | `/v1/dns/zones/{zone}` | Get zone details |
| DELETE | `/v1/dns/zones/{zone}` | Delete a zone |
| GET | `/v1/dns/zones/{zone}/records` | List records in a zone |
| POST | `/v1/dns/zones/{zone}/records` | Create a record |
| GET | `/v1/dns/zones/{zone}/records/{id}` | Get a record |
| PATCH | `/v1/dns/zones/{zone}/records/{id}` | Update a record |
| DELETE | `/v1/dns/zones/{zone}/records/{id}` | Delete a record |

## Record Types

A, AAAA, CNAME, MX, TXT, SRV, NS, SOA, CAA

## Examples

Enable the API on the default port:

```
. {
    hanzodns
    forward . 8.8.8.8
}
```

Listen on a custom port:

```
. {
    hanzodns :9443
    forward . 8.8.8.8
}
```

Create a zone and record via curl:

```bash
# Create zone
curl -X POST http://localhost:8443/v1/dns/zones \
  -H "Authorization: Bearer $HANZO_DNS_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"zone": "example.com"}'

# Add A record
curl -X POST http://localhost:8443/v1/dns/zones/example.com/records \
  -H "Authorization: Bearer $HANZO_DNS_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name": "www", "type": "A", "content": "1.2.3.4", "ttl": 300}'

# Query it
dig @localhost www.example.com A
```
