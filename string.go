package redis

import (
	"strconv"
	"time"
)

// SetOptions modifies the behavior of Set. The zero value performs an
// unconditional set with no expiration.
type SetOptions struct {
	// EX sets an expiration in seconds. Ignored if zero.
	EX time.Duration
	// PX sets an expiration in milliseconds. Ignored if zero. If both EX and
	// PX are set, PX takes precedence.
	PX time.Duration
	// NX sets the key only if it does not already exist.
	NX bool
	// XX sets the key only if it already exists.
	XX bool
}

// Set stores str at key subject to opts. It reports whether the write happened;
// a false result means an NX/XX precondition was not met. The key's type is
// reset to string and, unless an expiration option is given, any existing TTL
// is cleared (matching Redis SET semantics).
func (s *Store) Set(key, str string, opts SetOptions) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := s.getLive(key)
	if opts.NX && existing != nil {
		return false
	}
	if opts.XX && existing == nil {
		return false
	}

	it := &item{kind: TypeString, str: str}
	if opts.PX > 0 {
		it.expireAt = s.now().Add(opts.PX)
	} else if opts.EX > 0 {
		it.expireAt = s.now().Add(opts.EX)
	}
	s.data[key] = it
	return true
}

// Get returns the string stored at key. The boolean is false if the key is
// absent or expired. It returns ErrWrongType if the key holds a non-string.
func (s *Store) Get(key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return "", false, nil
	}
	if it.kind != TypeString {
		return "", false, ErrWrongType
	}
	return it.str, true, nil
}

// GetSet atomically sets key to str and returns the previous value. The boolean
// is false when there was no previous value.
func (s *Store) GetSet(key, str string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	var old string
	had := false
	if it != nil {
		if it.kind != TypeString {
			return "", false, ErrWrongType
		}
		old = it.str
		had = true
	}
	// GETSET clears any existing TTL.
	s.data[key] = &item{kind: TypeString, str: str}
	return old, had, nil
}

// Append appends str to the value at key, creating it if absent, and returns
// the resulting string length.
func (s *Store) Append(key, str string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		s.data[key] = &item{kind: TypeString, str: str}
		return len(str), nil
	}
	if it.kind != TypeString {
		return 0, ErrWrongType
	}
	it.str += str
	return len(it.str), nil
}

// Strlen returns the length of the string value at key, or 0 if absent.
func (s *Store) Strlen(key string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return 0, nil
	}
	if it.kind != TypeString {
		return 0, ErrWrongType
	}
	return len(it.str), nil
}

// IncrBy increments the integer value at key by delta and returns the result.
// A missing key is treated as 0. It returns ErrNotInteger if the value is not a
// base-10 integer.
func (s *Store) IncrBy(key string, delta int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	var cur int64
	if it != nil {
		if it.kind != TypeString {
			return 0, ErrWrongType
		}
		n, err := strconv.ParseInt(it.str, 10, 64)
		if err != nil {
			return 0, ErrNotInteger
		}
		cur = n
	}
	cur += delta
	if it != nil {
		it.str = strconv.FormatInt(cur, 10)
	} else {
		s.data[key] = &item{kind: TypeString, str: strconv.FormatInt(cur, 10)}
	}
	return cur, nil
}

// Incr increments the integer value at key by one.
func (s *Store) Incr(key string) (int64, error) { return s.IncrBy(key, 1) }

// Decr decrements the integer value at key by one.
func (s *Store) Decr(key string) (int64, error) { return s.IncrBy(key, -1) }

// DecrBy decrements the integer value at key by delta.
func (s *Store) DecrBy(key string, delta int64) (int64, error) { return s.IncrBy(key, -delta) }
