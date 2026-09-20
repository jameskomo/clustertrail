// Package store persists the last rows the engine saw for a subscription, so
// the next launch can paint a table before any cluster has answered.
//
// Cached rows are a picture of the past and are always labelled as such. The
// UI shows them immediately, then replaces them wholesale when the informer
// syncs. Nothing here is ever used to decide anything; it exists only so the
// window is useful in the first second.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jameskomo/clustertrail/engine/internal/rows"
)

// Snapshot is one subscription's rows as they were when last seen.
type Snapshot struct {
	Cluster   string     `json:"cluster"`
	Kind      string     `json:"kind"`
	Namespace string     `json:"namespace"`
	SavedAt   time.Time  `json:"savedAt"`
	Rows      []rows.Row `json:"rows"`
}

// maxRows keeps a cache file small enough to read in a few milliseconds. A
// table larger than this loses nothing important by starting empty.
const maxRows = 2000

// expiry is how old a snapshot may be and still be worth showing. A day-old
// picture of a cluster is misleading rather than helpful.
const expiry = 12 * time.Hour

// Store writes snapshots under a directory, one file per subscription.
type Store struct {
	dir string
	mu  sync.Mutex
}

func New(dir string) (*Store, error) {
	d := filepath.Join(dir, "snapshots")
	if err := os.MkdirAll(d, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: d}, nil
}

func (s *Store) path(cluster, kind, namespace string) string {
	sum := sha256.Sum256([]byte(cluster + "\x00" + kind + "\x00" + namespace))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:8])+".json")
}

// Save records the rows for a subscription, trimming anything oversized.
func (s *Store) Save(cluster, kind, namespace string, list []rows.Row) {
	if s == nil || len(list) == 0 || len(list) > maxRows {
		return
	}
	snap := Snapshot{Cluster: cluster, Kind: kind, Namespace: namespace, SavedAt: time.Now().UTC(), Rows: list}
	body, err := json.Marshal(snap)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Write to a temporary file and rename, so a crash mid-write cannot leave
	// a half-written cache that fails to parse on the next launch.
	final := s.path(cluster, kind, namespace)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, final)
}

// Load returns the cached rows for a subscription, or nil when there is
// nothing usable. A stale file is deleted rather than returned.
func (s *Store) Load(cluster, kind, namespace string) *Snapshot {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.path(cluster, kind, namespace)
	body, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var snap Snapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		_ = os.Remove(path)
		return nil
	}
	if time.Since(snap.SavedAt) > expiry {
		_ = os.Remove(path)
		return nil
	}
	return &snap
}

// Forget drops every cached snapshot. The UI offers this so a person who does
// not want yesterday's cluster on disk can remove it.
func (s *Store) Forget() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		_ = os.Remove(filepath.Join(s.dir, e.Name()))
	}
	return nil
}
