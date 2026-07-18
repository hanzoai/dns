package hanzodns

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// RecordType enumerates supported DNS record types.
type RecordType string

const (
	TypeA     RecordType = "A"
	TypeAAAA  RecordType = "AAAA"
	TypeCNAME RecordType = "CNAME"
	TypeMX    RecordType = "MX"
	TypeTXT   RecordType = "TXT"
	TypeSRV   RecordType = "SRV"
	TypeNS    RecordType = "NS"
	TypeSOA   RecordType = "SOA"
	TypeCAA   RecordType = "CAA"
)

// ValidRecordTypes is the set of record types accepted by the API.
var ValidRecordTypes = map[RecordType]bool{
	TypeA: true, TypeAAAA: true, TypeCNAME: true, TypeMX: true,
	TypeTXT: true, TypeSRV: true, TypeNS: true, TypeSOA: true, TypeCAA: true,
}

// Record represents a single DNS record.
type Record struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Type      RecordType `json:"type"`
	TTL       uint32     `json:"ttl"`
	Content   string     `json:"content"`
	Priority  uint16     `json:"priority,omitempty"`
	Proxied   bool       `json:"proxied"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Zone represents a DNS zone and its records.
type Zone struct {
	ID   string `json:"id"`
	Name string `json:"zone"`
	// Provider names the backend that serves this zone: "authoritative" (the
	// default) means this CoreDNS store answers it directly; "cloudflare" means
	// records are managed in the org's connected Cloudflare account.
	Provider      string    `json:"provider"`
	Status        string    `json:"status"`
	Nameservers   []string  `json:"nameservers"`
	RecordCount   int       `json:"record_count"`
	DNSSECEnabled bool      `json:"dnssec_enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Store provides thread-safe in-memory storage for DNS zones and records. It is
// the authoritative backend for zones whose provider is "authoritative", and a
// local index/serving cache for provider-backed zones. A durable snapshot
// (store_snapshot.go) can persist it across restarts.
type Store struct {
	mu    sync.RWMutex
	zones map[string]*zoneData // keyed by normalized zone name (e.g. "example.com.")

	// onChange, if set, is invoked (without the lock held) after any mutation so
	// a snapshot can be persisted. Wired by the snapshot layer; nil in tests.
	onChange func()
}

type zoneData struct {
	zone    Zone
	records map[string]*Record // keyed by record ID
}

// NewStore creates an empty Store.
func NewStore() *Store {
	return &Store{
		zones: make(map[string]*zoneData),
	}
}

// notify invokes the change hook (if any) so the durable snapshot is rewritten.
// Called after the write lock is released to keep persistence off the hot path
// of the lock.
func (s *Store) notify() {
	if s.onChange != nil {
		s.onChange()
	}
}

// normZone ensures the zone name ends with a dot (FQDN).
func normZone(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if !strings.HasSuffix(name, ".") {
		name += "."
	}
	return name
}

// ListZones returns all zones.
func (s *Store) ListZones() []Zone {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Zone, 0, len(s.zones))
	for _, zd := range s.zones {
		z := zd.zone
		z.RecordCount = len(zd.records)
		out = append(out, z)
	}
	return out
}

// GetZone returns a zone by name or nil if not found.
func (s *Store) GetZone(name string) *Zone {
	s.mu.RLock()
	defer s.mu.RUnlock()

	zd, ok := s.zones[normZone(name)]
	if !ok {
		return nil
	}
	z := zd.zone
	z.RecordCount = len(zd.records)
	return &z
}

// CreateZone adds a new zone served by the given provider ("authoritative" or
// "cloudflare"). Returns an error if it already exists.
func (s *Store) CreateZone(name, provider string) (*Zone, error) {
	key := normZone(name)

	s.mu.Lock()
	if _, exists := s.zones[key]; exists {
		s.mu.Unlock()
		return nil, fmt.Errorf("zone %q already exists", key)
	}

	now := time.Now().UTC()
	z := Zone{
		ID:          uuid.New().String(),
		Name:        key,
		Provider:    provider,
		Status:      "active",
		Nameservers: []string{"ns1.hanzo.ai.", "ns2.hanzo.ai."},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	s.zones[key] = &zoneData{
		zone:    z,
		records: make(map[string]*Record),
	}
	s.mu.Unlock()
	s.notify()
	return &z, nil
}

// ZoneProvider returns the provider serving the named zone and whether the zone
// exists. An empty provider is normalized to "authoritative".
func (s *Store) ZoneProvider(name string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	zd, ok := s.zones[normZone(name)]
	if !ok {
		return "", false
	}
	if zd.zone.Provider == "" {
		return ProviderAuthoritative, true
	}
	return zd.zone.Provider, true
}

// DeleteZone removes a zone and all its records.
func (s *Store) DeleteZone(name string) error {
	key := normZone(name)

	changed := false
	s.mu.Lock()
	defer func() {
		if changed {
			s.notify()
		}
	}()
	defer s.mu.Unlock()

	if _, exists := s.zones[key]; !exists {
		return fmt.Errorf("zone %q not found", key)
	}
	delete(s.zones, key)
	changed = true
	return nil
}

// ListRecords returns all records for a zone.
func (s *Store) ListRecords(zone string) ([]Record, error) {
	key := normZone(zone)

	s.mu.RLock()
	defer s.mu.RUnlock()

	zd, ok := s.zones[key]
	if !ok {
		return nil, fmt.Errorf("zone %q not found", key)
	}

	out := make([]Record, 0, len(zd.records))
	for _, r := range zd.records {
		out = append(out, *r)
	}
	return out, nil
}

// GetRecord returns a single record by zone and ID.
func (s *Store) GetRecord(zone, id string) (*Record, error) {
	key := normZone(zone)

	s.mu.RLock()
	defer s.mu.RUnlock()

	zd, ok := s.zones[key]
	if !ok {
		return nil, fmt.Errorf("zone %q not found", key)
	}
	r, ok := zd.records[id]
	if !ok {
		return nil, fmt.Errorf("record %q not found", id)
	}
	cp := *r
	return &cp, nil
}

// CreateRecord adds a record to a zone. Returns the created record.
func (s *Store) CreateRecord(zone string, name string, rtype RecordType, ttl uint32, content string, priority uint16, proxied bool) (*Record, error) {
	key := normZone(zone)

	if !ValidRecordTypes[rtype] {
		return nil, fmt.Errorf("invalid record type %q", rtype)
	}

	changed := false
	s.mu.Lock()
	defer func() {
		if changed {
			s.notify()
		}
	}()
	defer s.mu.Unlock()

	zd, ok := s.zones[key]
	if !ok {
		return nil, fmt.Errorf("zone %q not found", key)
	}

	now := time.Now().UTC()
	r := &Record{
		ID:        uuid.New().String(),
		Name:      name,
		Type:      rtype,
		TTL:       ttl,
		Content:   content,
		Priority:  priority,
		Proxied:   proxied,
		CreatedAt: now,
		UpdatedAt: now,
	}
	zd.records[r.ID] = r

	zd.zone.UpdatedAt = now
	changed = true

	cp := *r
	return &cp, nil
}

// PutMirror upserts a record with an explicit id into a zone's local index.
// It is used to mirror a provider-backed record (keyed by the PROVIDER's record
// id) into this store, so CoreDNS can serve the zone locally and the local API
// stays consistent with the provider. Unlike CreateRecord it neither generates
// an id nor rejects an existing one.
func (s *Store) PutMirror(zone string, rec ProviderRecord) error {
	key := normZone(zone)
	if rec.ID == "" {
		return fmt.Errorf("mirror record requires an id")
	}
	if !ValidRecordTypes[RecordType(rec.Type)] {
		return fmt.Errorf("invalid record type %q", rec.Type)
	}

	changed := false
	s.mu.Lock()
	defer func() {
		if changed {
			s.notify()
		}
	}()
	defer s.mu.Unlock()

	zd, ok := s.zones[key]
	if !ok {
		return fmt.Errorf("zone %q not found", key)
	}
	now := time.Now().UTC()
	existing, ok := zd.records[rec.ID]
	created := now
	if ok {
		created = existing.CreatedAt
	}
	zd.records[rec.ID] = &Record{
		ID:        rec.ID,
		Name:      rec.Name,
		Type:      RecordType(rec.Type),
		TTL:       rec.TTL,
		Content:   rec.Content,
		Priority:  rec.Priority,
		Proxied:   rec.Proxied,
		CreatedAt: created,
		UpdatedAt: now,
	}
	zd.zone.UpdatedAt = now
	changed = true
	return nil
}

// DropMirror removes a mirrored record by id if present. Absence is not an
// error (the local index is best-effort relative to the provider).
func (s *Store) DropMirror(zone, id string) {
	key := normZone(zone)
	changed := false
	s.mu.Lock()
	defer func() {
		if changed {
			s.notify()
		}
	}()
	defer s.mu.Unlock()
	zd, ok := s.zones[key]
	if !ok {
		return
	}
	if _, ok := zd.records[id]; ok {
		delete(zd.records, id)
		zd.zone.UpdatedAt = time.Now().UTC()
		changed = true
	}
}

// UpdateRecord patches a record. Only non-zero fields are updated.
func (s *Store) UpdateRecord(zone, id string, patch RecordPatch) (*Record, error) {
	key := normZone(zone)

	changed := false
	s.mu.Lock()
	defer func() {
		if changed {
			s.notify()
		}
	}()
	defer s.mu.Unlock()

	zd, ok := s.zones[key]
	if !ok {
		return nil, fmt.Errorf("zone %q not found", key)
	}
	r, ok := zd.records[id]
	if !ok {
		return nil, fmt.Errorf("record %q not found", id)
	}

	if patch.Name != nil {
		r.Name = *patch.Name
	}
	if patch.Type != nil {
		if !ValidRecordTypes[*patch.Type] {
			return nil, fmt.Errorf("invalid record type %q", *patch.Type)
		}
		r.Type = *patch.Type
	}
	if patch.TTL != nil {
		r.TTL = *patch.TTL
	}
	if patch.Content != nil {
		r.Content = *patch.Content
	}
	if patch.Priority != nil {
		r.Priority = *patch.Priority
	}
	if patch.Proxied != nil {
		r.Proxied = *patch.Proxied
	}
	r.UpdatedAt = time.Now().UTC()
	zd.zone.UpdatedAt = r.UpdatedAt
	changed = true

	cp := *r
	return &cp, nil
}

// DeleteRecord removes a record from a zone.
func (s *Store) DeleteRecord(zone, id string) error {
	key := normZone(zone)

	changed := false
	s.mu.Lock()
	defer func() {
		if changed {
			s.notify()
		}
	}()
	defer s.mu.Unlock()

	zd, ok := s.zones[key]
	if !ok {
		return fmt.Errorf("zone %q not found", key)
	}
	if _, ok := zd.records[id]; !ok {
		return fmt.Errorf("record %q not found", id)
	}
	delete(zd.records, id)
	zd.zone.UpdatedAt = time.Now().UTC()
	changed = true
	return nil
}

// RecordPatch carries optional fields for a partial record update.
type RecordPatch struct {
	Name     *string     `json:"name,omitempty"`
	Type     *RecordType `json:"type,omitempty"`
	TTL      *uint32     `json:"ttl,omitempty"`
	Content  *string     `json:"content,omitempty"`
	Priority *uint16     `json:"priority,omitempty"`
	Proxied  *bool       `json:"proxied,omitempty"`
}

// Lookup returns all records matching the given FQDN and record type string.
// Used by the DNS handler to serve queries from the store.
func (s *Store) Lookup(qname string, qtype string) []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()

	qname = strings.ToLower(qname)

	var matches []Record
	for zoneName, zd := range s.zones {
		if !strings.HasSuffix(qname, zoneName) {
			continue
		}
		for _, r := range zd.records {
			fqdn := s.fqdn(r.Name, zoneName)
			if strings.ToLower(fqdn) == qname && string(r.Type) == qtype {
				matches = append(matches, *r)
			}
		}
	}
	return matches
}

// ZoneNames returns all zone names the store knows about (FQDNs with trailing dot).
func (s *Store) ZoneNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.zones))
	for k := range s.zones {
		names = append(names, k)
	}
	return names
}

// fqdn converts a record name relative to a zone into an FQDN.
func (s *Store) fqdn(name, zone string) string {
	if name == "@" || name == "" {
		return zone
	}
	if strings.HasSuffix(name, ".") {
		return name
	}
	return name + "." + zone
}
