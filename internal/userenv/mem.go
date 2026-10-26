package userenv

import "sync"

// Mem is an in-memory user environment for tests.
type Mem struct {
	mu   sync.Mutex
	vals map[string]string
}

// NewMem returns an empty store.
func NewMem() *Mem {
	return &Mem{vals: map[string]string{}}
}

// Get implements Store.
func (m *Mem) Get(key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.vals[key]
	return v, ok, nil
}

// Set implements Store.
func (m *Mem) Set(key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.vals == nil {
		m.vals = map[string]string{}
	}
	m.vals[key] = value
	return nil
}

// Delete implements Store.
func (m *Mem) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.vals, key)
	return nil
}

var mems sync.Map

// MemFor returns the memory store bound to home for the life of the process.
// Repeated test setup for the same home keeps values already written.
func MemFor(home string) *Mem {
	v, _ := mems.LoadOrStore(home, NewMem())
	return v.(*Mem)
}
