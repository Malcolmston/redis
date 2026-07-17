package redis

import (
	"errors"
	"math/rand"
	"strconv"
	"time"
)

// ErrNoSuchKey is returned by commands that require the source key to exist,
// such as Rename, when that key is absent. It mirrors the Redis "no such key"
// error reply and is safe to compare with errors.Is.
var ErrNoSuchKey = errors.New("ERR no such key")

// copyItem returns a deep copy of src, duplicating whichever typed payload is
// active (string, list, hash, set, or sorted set) along with the expiration
// time so that mutations to the copy never affect the original. It returns nil
// when src is nil.
func copyItem(src *item) *item {
	if src == nil {
		return nil
	}
	dst := &item{kind: src.kind, expireAt: src.expireAt, str: src.str}
	if src.list != nil {
		dst.list = make([]string, len(src.list))
		copy(dst.list, src.list)
	}
	if src.hash != nil {
		dst.hash = make(map[string]string, len(src.hash))
		for k, v := range src.hash {
			dst.hash[k] = v
		}
	}
	if src.set != nil {
		dst.set = make(map[string]struct{}, len(src.set))
		for m := range src.set {
			dst.set[m] = struct{}{}
		}
	}
	if src.zset != nil {
		z := newZSet()
		for _, m := range src.zset.sl.toSlice() {
			z.add(m.Member, m.Score)
		}
		dst.zset = z
	}
	return dst
}

// extkeyspaceEncoding reports the object encoding name for it, derived from its
// kind (and, for strings, from whether the value is an integer and how long it
// is). It mirrors the encodings reported by Redis OBJECT ENCODING.
func extkeyspaceEncoding(it *item) string {
	switch it.kind {
	case TypeString:
		if _, err := strconv.ParseInt(it.str, 10, 64); err == nil {
			return "int"
		}
		if len(it.str) <= 44 {
			return "embstr"
		}
		return "raw"
	case TypeList:
		return "listpack"
	case TypeHash:
		return "hashtable"
	case TypeSet:
		return "hashtable"
	case TypeZSet:
		return "skiplist"
	default:
		return ""
	}
}

// SetRange overwrites part of the string at key starting at offset with val,
// zero-padding with NUL bytes if the current value is shorter than offset. A
// missing key is treated as an empty string. It returns the length of the
// resulting string. A negative offset yields ErrOutOfRange, and a non-string
// key yields ErrWrongType. When val is empty the value is left unchanged and
// its current length is returned.
func (s *Store) SetRange(key string, offset int, val string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if offset < 0 {
		return 0, ErrOutOfRange
	}
	it := s.getLive(key)
	var cur string
	if it != nil {
		if it.kind != TypeString {
			return 0, ErrWrongType
		}
		cur = it.str
	}
	if len(val) == 0 {
		return len(cur), nil
	}
	size := len(cur)
	if end := offset + len(val); end > size {
		size = end
	}
	buf := make([]byte, size)
	copy(buf, cur)
	copy(buf[offset:], val)
	result := string(buf)
	if it != nil {
		it.str = result
	} else {
		s.data[key] = &item{kind: TypeString, str: result}
	}
	return len(result), nil
}

// GetRange returns the substring of the string at key bounded by the inclusive
// indexes start and end. Negative indexes count back from the end of the
// string, and both bounds are clamped to the string's extent. A missing key,
// an empty string, or an empty range yields the empty string. A non-string key
// yields ErrWrongType.
func (s *Store) GetRange(key string, start, end int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return "", nil
	}
	if it.kind != TypeString {
		return "", ErrWrongType
	}
	str := it.str
	n := len(str)
	if n == 0 {
		return "", nil
	}
	if start < 0 {
		start += n
	}
	if end < 0 {
		end += n
	}
	if start < 0 {
		start = 0
	}
	if end >= n {
		end = n - 1
	}
	if start > end || start >= n || end < 0 {
		return "", nil
	}
	return str[start : end+1], nil
}

// SetEx sets key to val with an expiration of seconds seconds, replacing any
// existing value and TTL. A non-positive seconds yields ErrSyntax.
func (s *Store) SetEx(key string, seconds int, val string) error {
	if seconds <= 0 {
		return ErrSyntax
	}
	s.Set(key, val, SetOptions{EX: time.Duration(seconds) * time.Second})
	return nil
}

// PSetEx sets key to val with an expiration of millis milliseconds, replacing
// any existing value and TTL. A non-positive millis yields ErrSyntax.
func (s *Store) PSetEx(key string, millis int, val string) error {
	if millis <= 0 {
		return ErrSyntax
	}
	s.Set(key, val, SetOptions{PX: time.Duration(millis) * time.Millisecond})
	return nil
}

// SetNX sets key to val only if key does not already exist. It reports whether
// the write happened. The error is always nil and exists for signature parity
// with the other string commands.
func (s *Store) SetNX(key, val string) (bool, error) {
	return s.Set(key, val, SetOptions{NX: true}), nil
}

// MSet sets each key to its paired value from pairs, which must be a non-empty,
// even-length sequence of alternating key and value strings; any other length
// yields ErrWrongArgs. Each write clears any existing TTL, matching SET.
func (s *Store) MSet(pairs ...string) error {
	if len(pairs) == 0 || len(pairs)%2 != 0 {
		return ErrWrongArgs
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < len(pairs); i += 2 {
		s.data[pairs[i]] = &item{kind: TypeString, str: pairs[i+1]}
	}
	return nil
}

// MSetNX sets each key to its paired value from pairs only if none of the keys
// already exist; the operation is all-or-nothing. It reports whether the writes
// happened. pairs must be a non-empty, even-length sequence of alternating key
// and value strings; any other length yields ErrWrongArgs.
func (s *Store) MSetNX(pairs ...string) (bool, error) {
	if len(pairs) == 0 || len(pairs)%2 != 0 {
		return false, ErrWrongArgs
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < len(pairs); i += 2 {
		if s.getLive(pairs[i]) != nil {
			return false, nil
		}
	}
	for i := 0; i < len(pairs); i += 2 {
		s.data[pairs[i]] = &item{kind: TypeString, str: pairs[i+1]}
	}
	return true, nil
}

// MGet returns the string values of the given keys in order. Each element is
// nil when the corresponding key is missing, expired, or holds a non-string
// value.
func (s *Store) MGet(keys ...string) []*string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*string, len(keys))
	for i, k := range keys {
		it := s.getLive(k)
		if it == nil || it.kind != TypeString {
			continue
		}
		v := it.str
		out[i] = &v
	}
	return out
}

// IncrByFloat increments the floating-point value at key by delta and returns
// the result, treating a missing key as 0. The stored value is rewritten using
// the store's canonical float formatting. A non-string key yields ErrWrongType,
// and a value that does not parse as a float yields ErrNotFloat.
func (s *Store) IncrByFloat(key string, delta float64) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	var cur float64
	if it != nil {
		if it.kind != TypeString {
			return 0, ErrWrongType
		}
		f, err := strconv.ParseFloat(it.str, 64)
		if err != nil {
			return 0, ErrNotFloat
		}
		cur = f
	}
	cur += delta
	formatted := formatFloat(cur)
	if it != nil {
		it.str = formatted
	} else {
		s.data[key] = &item{kind: TypeString, str: formatted}
	}
	return cur, nil
}

// Rename moves the value at src, including any TTL, to dst, overwriting any
// existing value at dst. It returns ErrNoSuchKey if src does not exist. Renaming
// a key to itself is a no-op that succeeds.
func (s *Store) Rename(src, dst string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(src)
	if it == nil {
		return ErrNoSuchKey
	}
	if src == dst {
		return nil
	}
	s.data[dst] = it
	delete(s.data, src)
	return nil
}

// RenameNX moves the value at src, including any TTL, to dst only if dst does
// not already exist. It reports whether the rename happened and returns
// ErrNoSuchKey if src does not exist.
func (s *Store) RenameNX(src, dst string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(src)
	if it == nil {
		return false, ErrNoSuchKey
	}
	if src == dst {
		return false, nil
	}
	if s.getLive(dst) != nil {
		return false, nil
	}
	s.data[dst] = it
	delete(s.data, src)
	return true, nil
}

// Copy writes a deep copy of the value at src, including any TTL, to dst. When
// dst already exists it is overwritten only if replace is true. It reports
// whether the copy happened; a false result means src was absent, dst existed
// without replace, or src and dst were the same key. The error is always nil
// and exists for signature parity.
func (s *Store) Copy(src, dst string, replace bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(src)
	if it == nil {
		return false, nil
	}
	if src == dst {
		return false, nil
	}
	if !replace && s.getLive(dst) != nil {
		return false, nil
	}
	s.data[dst] = copyItem(it)
	return true, nil
}

// RandomKey returns a uniformly-random live key from the store. The boolean is
// false when the store holds no live keys.
func (s *Store) RandomKey() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	live := make([]string, 0, len(s.data))
	for k := range s.data {
		if s.getLive(k) != nil {
			live = append(live, k)
		}
	}
	if len(live) == 0 {
		return "", false
	}
	return live[rand.Intn(len(live))], true
}

// ObjectInfo reports internal metadata about the value stored at a key, as
// exposed by the Redis OBJECT command.
type ObjectInfo struct {
	// Encoding is the internal representation name, such as "embstr",
	// "listpack", "hashtable", or "skiplist".
	Encoding string
	// RefCount is the number of references to the value; this store keeps one
	// reference per key.
	RefCount int64
	// IdleTime is the number of seconds the value has been idle. This store
	// does not track access time, so it is always zero.
	IdleTime int64
}

// Object returns metadata about the value stored at key. The boolean is false
// when the key is missing or expired.
func (s *Store) Object(key string) (ObjectInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return ObjectInfo{}, false
	}
	return ObjectInfo{
		Encoding: extkeyspaceEncoding(it),
		RefCount: 1,
		IdleTime: 0,
	}, true
}

// Touch returns the number of the given keys that currently exist. Keys given
// multiple times are counted multiple times, matching Redis semantics. It does
// not alter the keys.
func (s *Store) Touch(keys ...string) int {
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

// Unlink removes the given keys and returns the count actually removed. It has
// the same semantics as Del.
func (s *Store) Unlink(keys ...string) int {
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
