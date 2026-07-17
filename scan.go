package redis

import "sort"

// scanDefaultCount is the number of elements a scan advances over per call when
// the caller does not request a positive COUNT.
const scanDefaultCount = 10

// ScanResult is the reply to a keyspace or set iteration step. Cursor is the
// opaque value to pass to the next call and is 0 once iteration is complete.
type ScanResult struct {
	// Cursor is the start index for the next call, or 0 when iteration is done.
	Cursor uint64
	// Keys holds the keys or set members produced by this step.
	Keys []string
}

// ScanPairs is the reply to a hash or sorted-set iteration step. It carries a
// flat sequence of pairs: field,value,... for hashes and member,score,... for
// sorted sets. Cursor is 0 once iteration is complete.
type ScanPairs struct {
	// Cursor is the start index for the next call, or 0 when iteration is done.
	Cursor uint64
	// Pairs holds the flattened field/value or member/score pairs from this step.
	Pairs []string
}

// scanBounds interprets cursor as a start index into a sorted list of length n
// and returns the half-open slice bounds [start,end) to emit this call along
// with the next cursor. next is 0 when the end of the list is reached or when
// cursor is out of range, signalling that iteration is complete. count values
// of zero or below fall back to scanDefaultCount.
func scanBounds(n int, cursor uint64, count int) (start, end int, next uint64) {
	if count <= 0 {
		count = scanDefaultCount
	}
	start = int(cursor)
	if start < 0 || start >= n {
		return 0, 0, 0
	}
	end = start + count
	if end >= n {
		return start, n, 0
	}
	return start, end, uint64(end)
}

// scanNames advances over the sorted names starting at cursor, keeping up to
// count of them and retaining only those matching the glob pattern (an empty
// pattern matches everything). It returns the surviving names, which may number
// fewer than count, and the next cursor (0 when iteration is complete).
func scanNames(names []string, cursor uint64, match string, count int) ([]string, uint64) {
	start, end, next := scanBounds(len(names), cursor, count)
	matched := make([]string, 0, end-start)
	for _, name := range names[start:end] {
		if match == "" || Match(match, name) {
			matched = append(matched, name)
		}
	}
	return matched, next
}

// scanSortedFields returns the keys of a hash's field map in ascending order.
func scanSortedFields(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for f := range m {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// scanSortedMembers returns the members of a sorted set's dict in ascending
// order.
func scanSortedMembers(m map[string]float64) []string {
	out := make([]string, 0, len(m))
	for member := range m {
		out = append(out, member)
	}
	sort.Strings(out)
	return out
}

// Scan iterates the live top-level keys of the store. cursor is the opaque
// value returned by the previous call (0 to begin). Expired keys are skipped,
// the optional match glob filters keys (empty matches all), and count bounds
// how many keys are examined this step (defaulting to 10 when count<=0). The
// returned ScanResult carries the matching keys and a Cursor of 0 once the
// final batch has been delivered.
func (s *Store) Scan(cursor uint64, match string, count int) ScanResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.data))
	for k, it := range s.data {
		if it.hasTTL() && !s.now().Before(it.expireAt) {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	matched, next := scanNames(keys, cursor, match, count)
	return ScanResult{Cursor: next, Keys: matched}
}

// HScan iterates the fields of the hash at key, returning field/value pairs
// flattened into ScanPairs.Pairs. The match glob filters on field name (empty
// matches all) and count bounds how many fields are examined this step
// (defaulting to 10 when count<=0). A missing key yields an empty result with
// Cursor 0; ErrWrongType is returned if key holds a non-hash value.
func (s *Store) HScan(key string, cursor uint64, match string, count int) (ScanPairs, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return ScanPairs{Cursor: 0, Pairs: []string{}}, nil
	}
	if it.kind != TypeHash {
		return ScanPairs{}, ErrWrongType
	}
	fields := scanSortedFields(it.hash)
	matched, next := scanNames(fields, cursor, match, count)
	pairs := make([]string, 0, len(matched)*2)
	for _, f := range matched {
		pairs = append(pairs, f, it.hash[f])
	}
	return ScanPairs{Cursor: next, Pairs: pairs}, nil
}

// SScan iterates the members of the set at key. The match glob filters on
// member (empty matches all) and count bounds how many members are examined
// this step (defaulting to 10 when count<=0). A missing key yields an empty
// result with Cursor 0; ErrWrongType is returned if key holds a non-set value.
func (s *Store) SScan(key string, cursor uint64, match string, count int) (ScanResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return ScanResult{Cursor: 0, Keys: []string{}}, nil
	}
	if it.kind != TypeSet {
		return ScanResult{}, ErrWrongType
	}
	members := sortedKeys(it.set)
	matched, next := scanNames(members, cursor, match, count)
	return ScanResult{Cursor: next, Keys: matched}, nil
}

// ZScan iterates the members of the sorted set at key, returning member/score
// pairs flattened into ScanPairs.Pairs with scores rendered by formatFloat. The
// match glob filters on member (empty matches all) and count bounds how many
// members are examined this step (defaulting to 10 when count<=0). A missing
// key yields an empty result with Cursor 0; ErrWrongType is returned if key
// holds a non-zset value.
func (s *Store) ZScan(key string, cursor uint64, match string, count int) (ScanPairs, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return ScanPairs{Cursor: 0, Pairs: []string{}}, nil
	}
	if it.kind != TypeZSet {
		return ScanPairs{}, ErrWrongType
	}
	members := scanSortedMembers(it.zset.dict)
	matched, next := scanNames(members, cursor, match, count)
	pairs := make([]string, 0, len(matched)*2)
	for _, m := range matched {
		pairs = append(pairs, m, formatFloat(it.zset.dict[m]))
	}
	return ScanPairs{Cursor: next, Pairs: pairs}, nil
}
