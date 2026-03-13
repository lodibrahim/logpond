package store

import (
	"sync"

	"github.com/lodibrahim/logpond/internal/parser"
)

type Store struct {
	mu       sync.RWMutex
	entries  []*parser.Entry
	capacity int
	head     int
	count    int
}

func New(capacity int) *Store {
	return &Store{
		entries:  make([]*parser.Entry, capacity),
		capacity: capacity,
	}
}

func (s *Store) Append(entry *parser.Entry) {
	s.mu.Lock()
	s.entries[s.head] = entry
	s.head = (s.head + 1) % s.capacity
	if s.count < s.capacity {
		s.count++
	}
	s.mu.Unlock()
}

func (s *Store) Clear() {
	s.mu.Lock()
	s.head = 0
	s.count = 0
	s.mu.Unlock()
}

func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.count
}

func (s *Store) Tail(n int) []*parser.Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tailLocked(n)
}

func (s *Store) All() []*parser.Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tailLocked(s.count)
}

// ForEach iterates all entries in chronological order under a read lock.
// The callback must not retain the entry pointer.
func (s *Store) ForEach(fn func(*parser.Entry)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.count == 0 {
		return
	}
	start := (s.head - s.count + s.capacity) % s.capacity
	for i := 0; i < s.count; i++ {
		fn(s.entries[(start+i)%s.capacity])
	}
}

func (s *Store) tailLocked(n int) []*parser.Entry {
	if n > s.count {
		n = s.count
	}
	if n == 0 {
		return nil
	}
	result := make([]*parser.Entry, n)
	start := (s.head - n + s.capacity) % s.capacity
	for i := 0; i < n; i++ {
		result[i] = s.entries[(start+i)%s.capacity]
	}
	return result
}
