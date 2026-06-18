package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type persistedState struct {
	Workloads map[string]WorkloadSpec `json:"workloads"`
	Audit     []AuditRecord           `json:"audit"`
	Events    []Event                 `json:"events"`
	NextID    int64                   `json:"nextId"`
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
			Workloads: map[string]WorkloadSpec{},
			Audit:     []AuditRecord{},
			Events:    []Event{},
			NextID:    1,
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
		store.state.Workloads = map[string]WorkloadSpec{}
	}
	return store, nil
}

func (s *Store) ListWorkloads() []WorkloadSpec {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]WorkloadSpec, 0, len(s.state.Workloads))
	for _, spec := range s.state.Workloads {
		out = append(out, spec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) GetWorkload(name string) (WorkloadSpec, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	spec, ok := s.state.Workloads[name]
	return spec, ok
}

func (s *Store) PutWorkload(spec WorkloadSpec) error {
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

func (s *Store) AddAudit(record AuditRecord) (AuditRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record.ID = s.state.NextID
	record.Time = time.Now().UTC()
	s.state.NextID++
	s.state.Audit = append([]AuditRecord{record}, s.state.Audit...)
	if len(s.state.Audit) > 500 {
		s.state.Audit = s.state.Audit[:500]
	}
	return record, s.saveLocked()
}

func (s *Store) ListAudit() []AuditRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AuditRecord, len(s.state.Audit))
	copy(out, s.state.Audit)
	return out
}

func (s *Store) AddEvent(event Event) (Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	event.ID = s.state.NextID
	event.Time = time.Now().UTC()
	s.state.NextID++
	s.state.Events = append([]Event{event}, s.state.Events...)
	if len(s.state.Events) > 500 {
		s.state.Events = s.state.Events[:500]
	}
	return event, s.saveLocked()
}

func (s *Store) ListEvents() []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Event, len(s.state.Events))
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
