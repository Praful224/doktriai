package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/praful224/doktriai/doktriai-packages"
)

type persistedState struct {
	Workloads map[string]packages.WorkloadSpec `json:"workloads"`
	Audit     []packages.AuditRecord           `json:"audit"`
	Events    []packages.Event                 `json:"events"`
	NextID    int64                            `json:"nextId"`
	NextSeq   int64                            `json:"nextSeq"` // monotonic sequence for audit
	Lock      packages.LockState               `json:"lock"`
}

type Store struct {
	mu    sync.RWMutex
	path  string
	state persistedState
}

func OpenStore(path string) (*Store, error) {
	store := &Store{
		path: path,
		state: persistedState{
			Workloads: map[string]packages.WorkloadSpec{},
			Audit:     []packages.AuditRecord{},
			Events:    []packages.Event{},
			NextID:    1,
			Lock:      packages.LockState{Locked: false},
		},
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, store.saveLocked()
	}
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &store.state); err != nil {
			return nil, err
		}
	}
	if store.state.Workloads == nil {
		store.state.Workloads = map[string]packages.WorkloadSpec{}
	}
	if len(store.state.Workloads) == 0 && len(store.state.Audit) == 0 && len(store.state.Events) == 0 {
		store.state.Workloads["secure-ingress"] = packages.WorkloadSpec{
			Name:          "secure-ingress",
			Image:         "nginx:alpine",
			Replicas:      2,
			Port:          8080,
			ContainerPort: 80,
			Runtime:       "docker",
		}
		store.state.Workloads["reconciler-daemon"] = packages.WorkloadSpec{
			Name:          "reconciler-daemon",
			Image:         "busybox:latest",
			Replicas:      1,
			Port:          0,
			ContainerPort: 0,
			Runtime:       "docker",
		}
		store.state.Workloads["agent-gateway"] = packages.WorkloadSpec{
			Name:          "agent-gateway",
			Image:         "python:3.11-alpine",
			Replicas:      1,
			Port:          9000,
			ContainerPort: 9000,
			Runtime:       "docker",
		}

		store.state.Audit = []packages.AuditRecord{
			{ID: 1, Time: time.Now().Add(-15 * time.Minute).UTC(), Actor: "admin", Action: "apply", Workload: "secure-ingress", Allowed: true},
			{ID: 2, Time: time.Now().Add(-14 * time.Minute).UTC(), Actor: "admin", Action: "apply", Workload: "reconciler-daemon", Allowed: true},
			{ID: 3, Time: time.Now().Add(-10 * time.Minute).UTC(), Actor: "agent:claude", Action: "apply", Workload: "evil-attacker/exploit", Allowed: false, Reason: "Agent Intent Guard block: reference image prefix forbidden"},
			{ID: 4, Time: time.Now().Add(-5 * time.Minute).UTC(), Actor: "operator", Action: "lock", Workload: "system", Allowed: true, Reason: "System maintenance window"},
			{ID: 5, Time: time.Now().Add(-2 * time.Minute).UTC(), Actor: "operator", Action: "unlock", Workload: "system", Allowed: true},
		}

		store.state.Events = []packages.Event{
			{ID: 6, Time: time.Now().Add(-1 * time.Minute).UTC(), Level: "ok", Source: "core", Workload: "secure-ingress", Message: "reconciler sync complete: replicas 2/2 online"},
			{ID: 7, Time: time.Now().Add(-2 * time.Minute).UTC(), Level: "ok", Source: "core", Workload: "reconciler-daemon", Message: "reconciler sync complete: replica 1/1 online"},
			{ID: 8, Time: time.Now().Add(-5 * time.Minute).UTC(), Level: "warn", Source: "api", Message: "environment locked by operator: 'System maintenance window'"},
			{ID: 9, Time: time.Now().Add(-10 * time.Minute).UTC(), Level: "error", Source: "api", Message: "Agent Intent Guard block: reference image prefix forbidden"},
		}
		store.state.NextID = 10
		_ = store.saveLocked()
	}
	return store, nil
}

func (s *Store) ListWorkloads() []packages.WorkloadSpec {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]packages.WorkloadSpec, 0, len(s.state.Workloads))
	for _, spec := range s.state.Workloads {
		out = append(out, spec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) GetWorkload(name string) (packages.WorkloadSpec, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	spec, ok := s.state.Workloads[name]
	return spec, ok
}

func (s *Store) PutWorkload(spec packages.WorkloadSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Workloads[spec.Name] = spec
	return s.saveLocked()
}

func (s *Store) DeleteWorkload(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state.Workloads, name)
	return s.saveLocked()
}

func (s *Store) AddAudit(record packages.AuditRecord) (packages.AuditRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record.ID = s.state.NextID
	record.SeqID = s.state.NextSeq
	record.Time = time.Now().UTC()
	s.state.NextID++
	s.state.NextSeq++
	s.state.Audit = append([]packages.AuditRecord{record}, s.state.Audit...)
	if len(s.state.Audit) > 500 {
		s.state.Audit = s.state.Audit[:500]
	}
	return record, s.saveLocked()
}

// GetAuditSince returns all audit records with SeqID >= sinceSeq.
// Callers can stream audit incrementally for SIEM integration.
func (s *Store) GetAuditSince(sinceSeq int64) []packages.AuditRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []packages.AuditRecord
	for _, r := range s.state.Audit {
		if r.SeqID >= sinceSeq {
			out = append(out, r)
		}
	}
	return out
}

// SnapshotHash computes a SHA256 hash of the current desired workload state.
// Used to populate StateHashBefore/After in audit records (Layer 3 — ASI06/ASI10).
func (s *Store) SnapshotHash() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, _ := json.Marshal(s.state.Workloads)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func (s *Store) ListAudit() []packages.AuditRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]packages.AuditRecord, len(s.state.Audit))
	copy(out, s.state.Audit)
	return out
}

func (s *Store) AddEvent(event packages.Event) (packages.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	event.ID = s.state.NextID
	event.Time = time.Now().UTC()
	s.state.NextID++
	s.state.Events = append([]packages.Event{event}, s.state.Events...)
	if len(s.state.Events) > 500 {
		s.state.Events = s.state.Events[:500]
	}
	return event, s.saveLocked()
}

func (s *Store) ListEvents() []packages.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]packages.Event, len(s.state.Events))
	copy(out, s.state.Events)
	return out
}

func (s *Store) saveLocked() error {
	tmp := s.path + ".tmp"
	body, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) GetLock() packages.LockState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Lock
}

func (s *Store) AcquireLock(actor, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Lock = packages.LockState{
		Locked:     true,
		AcquiredBy: actor,
		Time:       time.Now().UTC(),
		Reason:     reason,
	}
	return s.saveLocked()
}

func (s *Store) ReleaseLock() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Lock = packages.LockState{
		Locked: false,
	}
	return s.saveLocked()
}
