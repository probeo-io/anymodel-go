package anymodel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// BatchStore handles disk persistence for batches.
type BatchStore struct {
	mu      sync.Mutex
	baseDir string
}

// NewBatchStore creates a new batch store.
func NewBatchStore(baseDir string) *BatchStore {
	if baseDir == "" {
		baseDir = filepath.Join(".", ".anymodel", "batches")
	}
	return &BatchStore{baseDir: baseDir}
}

func (s *BatchStore) batchDir(id string) string {
	return filepath.Join(s.baseDir, id)
}

// Create creates a new batch directory and writes metadata.
func (s *BatchStore) Create(batch BatchObject) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.batchDir(batch.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return s.writeJSON(filepath.Join(dir, "meta.json"), batch)
}

// GetMeta reads batch metadata.
func (s *BatchStore) GetMeta(id string) (*BatchObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.batchDir(id), "meta.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var batch BatchObject
	if err := json.Unmarshal(data, &batch); err != nil {
		return nil, err
	}
	return &batch, nil
}

// UpdateMeta atomically writes batch metadata.
func (s *BatchStore) UpdateMeta(batch BatchObject) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeJSON(filepath.Join(s.batchDir(batch.ID), "meta.json"), batch)
}

// SaveRequests writes batch requests as JSONL.
func (s *BatchStore) SaveRequests(id string, requests []BatchRequestItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.batchDir(id), "requests.jsonl")
	var lines []string
	for _, r := range requests {
		data, err := json.Marshal(r)
		if err != nil {
			return err
		}
		lines = append(lines, string(data))
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// AppendResult appends a result to the results JSONL file.
func (s *BatchStore) AppendResult(id string, result BatchResultItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.batchDir(id), "results.jsonl")
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(string(data) + "\n")
	return err
}

// GetResults reads all results from a batch.
func (s *BatchStore) GetResults(id string) ([]BatchResultItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.batchDir(id), "results.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var results []BatchResultItem
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var r BatchResultItem
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue
		}
		results = append(results, r)
	}
	return results, nil
}

// SaveProviderState writes provider-specific state.
func (s *BatchStore) SaveProviderState(id string, state map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeJSON(filepath.Join(s.batchDir(id), "provider.json"), state)
}

// LoadProviderState reads provider-specific state.
func (s *BatchStore) LoadProviderState(id string) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.batchDir(id), "provider.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return state, nil
}

// ListBatches returns all batch IDs on disk.
func (s *BatchStore) ListBatches() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "batch-") {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

func (s *BatchStore) writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return err
	}
	tmpFile.Close()
	return os.Rename(tmpPath, path)
}
