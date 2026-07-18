package hanzodns

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// snapshotFile is the state file name written under the configured state dir.
const snapshotFile = "hanzodns-state.json"

// snapshotZone is a zone plus its records in serializable form.
type snapshotZone struct {
	Zone    Zone     `json:"zone"`
	Records []Record `json:"records"`
}

// snapshotState is the whole store rendered for the durable snapshot.
type snapshotState struct {
	Zones []snapshotZone `json:"zones"`
}

// Export captures a consistent point-in-time copy of the store. Zones and
// records are sorted so the serialized snapshot is stable across writes.
func (s *Store) Export() snapshotState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.zones))
	for name := range s.zones {
		names = append(names, name)
	}
	sort.Strings(names)

	out := snapshotState{Zones: make([]snapshotZone, 0, len(names))}
	for _, name := range names {
		zd := s.zones[name]
		recs := make([]Record, 0, len(zd.records))
		for _, r := range zd.records {
			recs = append(recs, *r)
		}
		sort.Slice(recs, func(i, j int) bool { return recs[i].ID < recs[j].ID })
		out.Zones = append(out.Zones, snapshotZone{Zone: zd.zone, Records: recs})
	}
	return out
}

// Import replaces the store's contents with the snapshot. Used once at startup
// to rehydrate authoritative (and mirrored) zones after a restart.
func (s *Store) Import(state snapshotState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.zones = make(map[string]*zoneData, len(state.Zones))
	for _, sz := range state.Zones {
		key := normZone(sz.Zone.Name)
		zd := &zoneData{zone: sz.Zone, records: make(map[string]*Record, len(sz.Records))}
		zd.zone.Name = key
		if zd.zone.Provider == "" {
			zd.zone.Provider = ProviderAuthoritative
		}
		for i := range sz.Records {
			r := sz.Records[i]
			zd.records[r.ID] = &r
		}
		s.zones[key] = zd
	}
}

// snapshotter persists a Store to a single JSON file. Writes are serialized and
// atomic (temp file + rename), so a crash mid-write never corrupts the state.
type snapshotter struct {
	path  string
	store *Store
	mu    sync.Mutex
}

// EnableSnapshot makes the store durable under dir: it loads an existing
// snapshot (if any) at startup and rewrites the snapshot after every mutation.
// When dir is empty the store stays purely in-memory (the prior behavior), so
// durability is opt-in via HANZO_DNS_STATE_DIR without changing defaults.
func EnableSnapshot(store *Store, dir string) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("hanzodns: create state dir: %w", err)
	}
	snap := &snapshotter{path: filepath.Join(dir, snapshotFile), store: store}

	if err := snap.load(); err != nil {
		return err
	}
	store.onChange = snap.save
	return nil
}

// load reads the snapshot into the store if the file exists. A missing file is
// a clean first boot, not an error.
func (s *snapshotter) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("hanzodns: read snapshot: %w", err)
	}
	var state snapshotState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("hanzodns: parse snapshot: %w", err)
	}
	s.store.Import(state)
	return nil
}

// save writes the current store state atomically. It is the store's onChange
// hook, invoked after each mutation once the store lock is released.
func (s *snapshotter) save() {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(s.store.Export(), "", "  ")
	if err != nil {
		log.Errorf("hanzodns: marshal snapshot: %s", err)
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		log.Errorf("hanzodns: write snapshot: %s", err)
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		log.Errorf("hanzodns: commit snapshot: %s", err)
		_ = os.Remove(tmp)
	}
}
