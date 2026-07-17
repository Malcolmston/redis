package redis

import (
	"sync"
	"time"
)

// Clock abstracts time retrieval so expiration can be tested deterministically.
// The zero interface is never used; the store defaults to a real-time clock.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
}

// realClock is the default Clock backed by time.Now.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// ManualClock is a Clock whose time is advanced explicitly. It is safe for
// concurrent use and is intended for deterministic expiration tests.
type ManualClock struct {
	mu sync.Mutex
	t  time.Time
}

// NewManualClock returns a ManualClock initialized to t.
func NewManualClock(t time.Time) *ManualClock { return &ManualClock{t: t} }

// Now returns the clock's current time.
func (c *ManualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Advance moves the clock forward by d.
func (c *ManualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// Set sets the clock to an absolute time.
func (c *ManualClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

// Type identifies the data type stored at a key.
type Type string

// Recognized value types, mirroring Redis TYPE replies.
const (
	TypeNone   Type = "none"
	TypeString Type = "string"
	TypeList   Type = "list"
	TypeHash   Type = "hash"
	TypeSet    Type = "set"
	TypeZSet   Type = "zset"
)

// item is the internal container for a single keyed value. exactly one of the
// typed fields is populated, selected by kind.
type item struct {
	kind Type
	// expireAt is the absolute expiration time; the zero value means no TTL.
	expireAt time.Time

	str  string
	list []string
	hash map[string]string
	set  map[string]struct{}
	zset *zset
}

func (it *item) hasTTL() bool { return !it.expireAt.IsZero() }

// Store is a thread-safe, in-memory keyspace holding typed values. The zero
// value is not usable; construct one with New or NewWithClock.
type Store struct {
	mu    sync.Mutex
	data  map[string]*item
	clock Clock
}

// New returns an empty Store backed by a real-time clock.
func New() *Store { return NewWithClock(realClock{}) }

// NewWithClock returns an empty Store that reads the current time from clock.
// Passing a *ManualClock makes expiration fully deterministic.
func NewWithClock(clock Clock) *Store {
	if clock == nil {
		clock = realClock{}
	}
	return &Store{data: make(map[string]*item), clock: clock}
}

// now returns the store's current time. Callers must hold mu.
func (s *Store) now() time.Time { return s.clock.Now() }

// getLive returns the live item at key, lazily deleting it if expired. It
// returns nil if the key is absent or expired. Callers must hold mu.
func (s *Store) getLive(key string) *item {
	it, ok := s.data[key]
	if !ok {
		return nil
	}
	if it.hasTTL() && !s.now().Before(it.expireAt) {
		delete(s.data, key)
		return nil
	}
	return it
}

// Del removes the given keys, returning the count of keys actually deleted.
func (s *Store) Del(keys ...string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, k := range keys {
		if s.getLive(k) != nil {
			delete(s.data, k)
			n++
		}
	}
	return n
}

// Exists returns the number of the given keys that currently exist. Keys given
// multiple times are counted multiple times, matching Redis semantics.
func (s *Store) Exists(keys ...string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, k := range keys {
		if s.getLive(k) != nil {
			n++
		}
	}
	return n
}

// TypeOf returns the Type of the value stored at key, or TypeNone if absent.
func (s *Store) TypeOf(key string) Type {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return TypeNone
	}
	return it.kind
}

// Keys returns all keys matching the glob-style pattern (see Match). The order
// of the returned slice is unspecified.
func (s *Store) Keys(pattern string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0)
	for k, it := range s.data {
		if it.hasTTL() && !s.now().Before(it.expireAt) {
			continue
		}
		if Match(pattern, k) {
			out = append(out, k)
		}
	}
	return out
}

// DBSize returns the number of live keys in the store.
func (s *Store) DBSize() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, it := range s.data {
		if it.hasTTL() && !s.now().Before(it.expireAt) {
			continue
		}
		n++
	}
	return n
}

// FlushAll removes every key from the store.
func (s *Store) FlushAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = make(map[string]*item)
}
