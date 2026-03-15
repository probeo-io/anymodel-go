package anymodel

import "sync"

// GenerationStatsStore is an in-memory store for generation stats.
type GenerationStatsStore struct {
	mu      sync.RWMutex
	entries []GenerationStats
	byID    map[string]int
	max     int
}

// NewGenerationStatsStore creates a new store with a max capacity.
func NewGenerationStatsStore(max int) *GenerationStatsStore {
	return &GenerationStatsStore{
		entries: make([]GenerationStats, 0, max),
		byID:    make(map[string]int, max),
		max:     max,
	}
}

// Record adds a generation stats entry.
func (s *GenerationStatsStore) Record(stats GenerationStats) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.entries) >= s.max {
		oldest := s.entries[0]
		delete(s.byID, oldest.ID)
		s.entries = s.entries[1:]
		for i, e := range s.entries {
			s.byID[e.ID] = i
		}
	}

	s.byID[stats.ID] = len(s.entries)
	s.entries = append(s.entries, stats)
}

// Get returns stats for a generation by ID.
func (s *GenerationStatsStore) Get(id string) *GenerationStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	idx, ok := s.byID[id]
	if !ok {
		return nil
	}
	entry := s.entries[idx]
	return &entry
}

// List returns the most recent stats entries.
func (s *GenerationStatsStore) List(limit int) []GenerationStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > len(s.entries) {
		limit = len(s.entries)
	}
	result := make([]GenerationStats, limit)
	copy(result, s.entries[len(s.entries)-limit:])
	return result
}
