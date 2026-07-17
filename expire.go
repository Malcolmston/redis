package redis

import "time"

// Expire sets a time-to-live of d on key. It returns false if the key does not
// exist. A non-positive d deletes the key immediately (as Redis does).
func (s *Store) Expire(key string, d time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return false
	}
	if d <= 0 {
		delete(s.data, key)
		return true
	}
	it.expireAt = s.now().Add(d)
	return true
}

// PExpire is Expire with millisecond-precision semantics; it is provided for
// naming parity with Redis and behaves identically to Expire for a duration.
func (s *Store) PExpire(key string, d time.Duration) bool { return s.Expire(key, d) }

// TTL returns the remaining time-to-live of key. The Duration is meaningful
// only when the returned code is TTLValue.
func (s *Store) TTL(key string) (time.Duration, TTLCode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return 0, TTLNoKey
	}
	if !it.hasTTL() {
		return 0, TTLNoExpiry
	}
	return it.expireAt.Sub(s.now()), TTLValue
}

// PTTL is an alias of TTL; the remaining Duration can be inspected at any
// precision by the caller.
func (s *Store) PTTL(key string) (time.Duration, TTLCode) { return s.TTL(key) }

// Persist removes any expiration from key, returning true if a TTL was cleared.
func (s *Store) Persist(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil || !it.hasTTL() {
		return false
	}
	it.expireAt = time.Time{}
	return true
}

// TTLCode categorizes a TTL result, distinguishing a missing key from a key
// with no expiration.
type TTLCode int

// TTL result codes, corresponding to Redis's -2/-1/positive reply convention.
const (
	// TTLValue indicates the returned Duration is a real remaining TTL.
	TTLValue TTLCode = iota
	// TTLNoKey indicates the key does not exist (Redis reply -2).
	TTLNoKey
	// TTLNoExpiry indicates the key exists but has no TTL (Redis reply -1).
	TTLNoExpiry
)
