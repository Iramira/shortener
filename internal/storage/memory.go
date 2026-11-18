package storage

import "sync"

type Link struct {
	ID          string
	OriginalURL string
}

type Memory struct {
	mu     sync.RWMutex
	byID   map[string]string
	byUser map[string][]Link
}

func New() *Memory {
	return &Memory{
		byID:   make(map[string]string),
		byUser: make(map[string][]Link),
	}
}

// Add returns false if the ID is already taken. Both indexes change together.
func (m *Memory) Add(userID string, link Link) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.byID[link.ID]; exists {
		return false
	}
	m.byID[link.ID] = link.OriginalURL
	m.byUser[userID] = append(m.byUser[userID], link)
	return true
}

func (m *Memory) Lookup(id string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	original, ok := m.byID[id]
	return original, ok
}

func (m *Memory) List(userID string) []Link {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return append([]Link(nil), m.byUser[userID]...)
}
