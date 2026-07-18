package redis

import (
	"sort"
	"strconv"
	"time"
)

// ExpireAt sets the absolute expiration time of key to t. It returns false if
// the key does not exist. If t is in the past the key is deleted immediately.
// It mirrors the Redis EXPIREAT/PEXPIREAT commands.
func (s *Store) ExpireAt(key string, t time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return false
	}
	if !t.After(s.now()) {
		delete(s.data, key)
		return true
	}
	it.expireAt = t
	return true
}

// PExpireAt sets the absolute expiration time of key to t. It is identical to
// ExpireAt and is provided for naming parity with the Redis PEXPIREAT command.
func (s *Store) PExpireAt(key string, t time.Time) bool { return s.ExpireAt(key, t) }

// ExpireTime returns the absolute time at which key will expire. The returned
// TTLCode is TTLValue when the time is meaningful, TTLNoExpiry when the key
// exists but has no expiration, and TTLNoKey when the key is absent. It mirrors
// the Redis EXPIRETIME command.
func (s *Store) ExpireTime(key string) (time.Time, TTLCode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return time.Time{}, TTLNoKey
	}
	if !it.hasTTL() {
		return time.Time{}, TTLNoExpiry
	}
	return it.expireAt, TTLValue
}

// PExpireTime returns the absolute expiration time of key. It is identical to
// ExpireTime and is provided for naming parity with the Redis PEXPIRETIME
// command.
func (s *Store) PExpireTime(key string) (time.Time, TTLCode) { return s.ExpireTime(key) }

// ExpireCond selects a precondition for ExpireWith, mirroring the NX, XX, GT,
// and LT flags added to the Redis EXPIRE family in Redis 7.0.
type ExpireCond int

// Preconditions for ExpireWith.
const (
	// ExpireCondNone always applies the new expiration.
	ExpireCondNone ExpireCond = iota
	// ExpireCondNX applies only when the key currently has no expiration.
	ExpireCondNX
	// ExpireCondXX applies only when the key currently has an expiration.
	ExpireCondXX
	// ExpireCondGT applies only when the new expiration is later than the
	// current one; a key with no expiration is treated as expiring at infinity,
	// so GT never applies to it.
	ExpireCondGT
	// ExpireCondLT applies only when the new expiration is earlier than the
	// current one; a key with no expiration is treated as expiring at infinity,
	// so LT always applies to it.
	ExpireCondLT
)

// ExpireWith sets a time-to-live of d on key subject to the precondition cond,
// reporting whether the expiration was applied. It returns false if the key does
// not exist or the precondition is not met. A non-positive d deletes the key
// (when the precondition allows the write). It mirrors Redis EXPIRE with the
// NX/XX/GT/LT options.
func (s *Store) ExpireWith(key string, d time.Duration, cond ExpireCond) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return false
	}
	hasTTL := it.hasTTL()
	switch cond {
	case ExpireCondNX:
		if hasTTL {
			return false
		}
	case ExpireCondXX:
		if !hasTTL {
			return false
		}
	case ExpireCondGT:
		if !hasTTL {
			return false
		}
		if !s.now().Add(d).After(it.expireAt) {
			return false
		}
	case ExpireCondLT:
		if hasTTL && !s.now().Add(d).Before(it.expireAt) {
			return false
		}
	}
	if d <= 0 {
		delete(s.data, key)
		return true
	}
	it.expireAt = s.now().Add(d)
	return true
}

// SortOptions controls Sort and SortStore. The zero value sorts the elements
// numerically in ascending order and returns them all.
type SortOptions struct {
	// Alpha sorts elements lexically as strings instead of numerically. When
	// false, every element must parse as a float or ErrNotFloat is returned.
	Alpha bool
	// Desc reverses the ordering to descending.
	Desc bool
	// Limit enables the Offset/Count window; when false all elements are
	// returned.
	Limit bool
	// Offset is the number of sorted elements to skip when Limit is true.
	Offset int
	// Count is the maximum number of elements to return when Limit is true; a
	// negative Count returns every element after Offset.
	Count int
}

// Sort returns the elements of the list, set, or sorted set at key in sorted
// order, without modifying the key. Elements are compared numerically by
// default, or lexically when opts.Alpha is set. It returns ErrNotFloat when a
// numeric sort meets a non-numeric element and ErrWrongType if the key holds a
// string. It mirrors a common subset of the Redis SORT command (ordering and
// LIMIT; the BY and GET patterns are not supported).
func (s *Store) Sort(key string, opts SortOptions) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.genericextSort(key, opts)
}

// SortStore sorts the elements of the list, set, or sorted set at key as Sort
// does and stores the result as a list at dst, returning its length. An empty
// result deletes dst. It mirrors the STORE modifier of Redis SORT.
func (s *Store) SortStore(dst, key string, opts SortOptions) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sorted, err := s.genericextSort(key, opts)
	if err != nil {
		return 0, err
	}
	if len(sorted) == 0 {
		delete(s.data, dst)
		return 0, nil
	}
	s.data[dst] = &item{kind: TypeList, list: sorted}
	return len(sorted), nil
}

// genericextSort gathers, orders, and windows the elements at key. Callers must
// hold mu.
func (s *Store) genericextSort(key string, opts SortOptions) ([]string, error) {
	it := s.getLive(key)
	if it == nil {
		return []string{}, nil
	}
	var elems []string
	switch it.kind {
	case TypeList:
		elems = append(elems, it.list...)
	case TypeSet:
		elems = sortedKeys(it.set)
	case TypeZSet:
		for _, m := range it.zset.sl.toSlice() {
			elems = append(elems, m.Member)
		}
	default:
		return nil, ErrWrongType
	}

	if opts.Alpha {
		sort.SliceStable(elems, func(i, j int) bool {
			if opts.Desc {
				return elems[i] > elems[j]
			}
			return elems[i] < elems[j]
		})
	} else {
		nums := make([]float64, len(elems))
		for i, e := range elems {
			f, err := strconv.ParseFloat(e, 64)
			if err != nil {
				return nil, ErrNotFloat
			}
			nums[i] = f
		}
		idx := make([]int, len(elems))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(a, b int) bool {
			if opts.Desc {
				return nums[idx[a]] > nums[idx[b]]
			}
			return nums[idx[a]] < nums[idx[b]]
		})
		out := make([]string, len(elems))
		for i, j := range idx {
			out[i] = elems[j]
		}
		elems = out
	}

	if opts.Limit {
		elems = zrangeextStrWindow(elems, opts.Offset, opts.Count)
	}
	return elems, nil
}
