package hanzodns

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SyncZoneRequest represents a bulk zone sync payload. The owning org is NOT
// taken from the body — it is the caller's validated `owner` claim, applied by
// handleSync — so a sync can never target or overwrite another org's zone.
type SyncZoneRequest struct {
	Zone    string       `json:"zone"`
	Records []SyncRecord `json:"records"`
}

// SyncRecord is a record in a bulk sync request.
type SyncRecord struct {
	ID       string     `json:"id,omitempty"`
	Name     string     `json:"name"`
	Type     RecordType `json:"type"`
	TTL      uint32     `json:"ttl"`
	Content  string     `json:"content"`
	Priority uint16     `json:"priority,omitempty"`
	Proxied  bool       `json:"proxied"`
}

// SyncResponse is returned from the bulk sync endpoint.
type SyncResponse struct {
	Zone        string `json:"zone"`
	RecordCount int    `json:"record_count"`
	Created     int    `json:"created"`
	Deleted     int    `json:"deleted"`
}

// BulkSync atomically replaces all records for org's zone. If the zone does not
// exist it is created for org; if it exists but is owned by a DIFFERENT org the
// sync is refused (a caller can never overwrite another tenant's zone). Returns
// counts of records created and deleted.
func (s *Store) BulkSync(org string, req SyncZoneRequest) (*SyncResponse, error) {
	key := normZone(req.Zone)

	s.mu.Lock()
	defer s.notify()
	defer s.mu.Unlock()

	now := time.Now().UTC()

	zd, exists := s.zones[key]
	if exists {
		if zd.zone.Org != org {
			return nil, fmt.Errorf("zone %q already exists", key)
		}
	} else {
		zd = &zoneData{
			zone: Zone{
				ID:          uuid.New().String(),
				Name:        key,
				Org:         org,
				Status:      "active",
				Nameservers: []string{"ns1.hanzo.ai.", "ns2.hanzo.ai."},
				CreatedAt:   now,
				UpdatedAt:   now,
			},
			records: make(map[string]*Record),
		}
		s.zones[key] = zd
	}

	deleted := len(zd.records)

	// Replace all records atomically.
	newRecords := make(map[string]*Record, len(req.Records))
	for _, sr := range req.Records {
		if !ValidRecordTypes[sr.Type] {
			continue
		}
		id := sr.ID
		if id == "" {
			id = uuid.New().String()
		}
		ttl := sr.TTL
		if ttl == 0 {
			ttl = 300
		}
		newRecords[id] = &Record{
			ID:        id,
			Name:      sr.Name,
			Type:      sr.Type,
			TTL:       ttl,
			Content:   sr.Content,
			Priority:  sr.Priority,
			Proxied:   sr.Proxied,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}
	zd.records = newRecords
	zd.zone.UpdatedAt = now

	return &SyncResponse{
		Zone:        key,
		RecordCount: len(newRecords),
		Created:     len(newRecords),
		Deleted:     deleted,
	}, nil
}
