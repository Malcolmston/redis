package redis

import (
	"math"
	"math/rand"
	"sort"
	"strconv"
)

// aggregate combines two scores under a ZStoreOp. It is used by ZUnionStore and
// ZInterStore to fold scores from multiple sorted sets into a single value.
func extcollectionsAggregate(op ZStoreOp, a, b float64) float64 {
	switch op {
	case ZStoreMin:
		if b < a {
			return b
		}
		return a
	case ZStoreMax:
		if b > a {
			return b
		}
		return a
	default:
		return a + b
	}
}

// extcollectionsHashFields returns the field names of hash h in ascending order.
func extcollectionsHashFields(h map[string]string) []string {
	out := make([]string, 0, len(h))
	for f := range h {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// extcollectionsLexContains reports whether member falls within the lexical
// range r, honoring the inclusive/exclusive bounds and the -/+ infinities.
func extcollectionsLexContains(r LexRange, member string) bool {
	if !r.MinInf {
		if r.MinExclusive {
			if member <= r.Min {
				return false
			}
		} else if member < r.Min {
			return false
		}
	}
	if !r.MaxInf {
		if r.MaxExclusive {
			if member >= r.Max {
				return false
			}
		} else if member > r.Max {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// List commands
// ---------------------------------------------------------------------------

// LInsert inserts val into the list at key either before or after the first
// occurrence of pivot. It returns the new length on success, -1 when pivot is
// not present, and 0 when the key does not exist. It returns ErrWrongType if
// the key holds a non-list value.
func (s *Store) LInsert(key string, before bool, pivot, val string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.listItem(key, false)
	if err != nil {
		return 0, err
	}
	if it == nil {
		return 0, nil
	}
	idx := -1
	for i, v := range it.list {
		if v == pivot {
			idx = i
			break
		}
	}
	if idx == -1 {
		return -1, nil
	}
	pos := idx
	if !before {
		pos = idx + 1
	}
	it.list = append(it.list, "")
	copy(it.list[pos+1:], it.list[pos:])
	it.list[pos] = val
	return len(it.list), nil
}

// LSet sets the element at index in the list at key to val. Negative indexes
// count from the tail. It returns ErrNoSuchKey when the key is absent and
// ErrOutOfRange when index is outside the list. It returns ErrWrongType if the
// key holds a non-list value.
func (s *Store) LSet(key string, index int, val string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.listItem(key, false)
	if err != nil {
		return err
	}
	if it == nil {
		return ErrNoSuchKey
	}
	i := index
	if i < 0 {
		i += len(it.list)
	}
	if i < 0 || i >= len(it.list) {
		return ErrOutOfRange
	}
	it.list[i] = val
	return nil
}

// LRem removes elements equal to val from the list at key. When count is
// positive it removes up to count elements moving from head to tail; when
// negative it removes up to -count elements moving from tail to head; when zero
// it removes every matching element. It returns the number removed and deletes
// the key when the list becomes empty.
func (s *Store) LRem(key string, count int, val string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.listItem(key, false)
	if err != nil || it == nil {
		return 0, err
	}
	n := len(it.list)
	keep := make([]bool, n)
	for i := range keep {
		keep[i] = true
	}
	removed := 0
	if count >= 0 {
		limit := count
		for i := 0; i < n; i++ {
			if it.list[i] == val && (limit == 0 || removed < limit) {
				keep[i] = false
				removed++
			}
		}
	} else {
		limit := -count
		for i := n - 1; i >= 0; i-- {
			if it.list[i] == val && removed < limit {
				keep[i] = false
				removed++
			}
		}
	}
	if removed > 0 {
		out := make([]string, 0, n-removed)
		for i, v := range it.list {
			if keep[i] {
				out = append(out, v)
			}
		}
		it.list = out
	}
	if len(it.list) == 0 {
		delete(s.data, key)
	}
	return removed, nil
}

// ListEnd identifies which end of a list an element is popped from or pushed to.
type ListEnd int

// The two ends of a list, used by LMove.
const (
	// ListLeft is the head of the list.
	ListLeft ListEnd = iota
	// ListRight is the tail of the list.
	ListRight
)

// LMove atomically pops an element from the from end of the list at src and
// pushes it onto the to end of the list at dst, which may equal src. It returns
// the moved element and true, or false when src is empty or absent. It returns
// ErrWrongType if either key holds a non-list value.
func (s *Store) LMove(src, dst string, from, to ListEnd) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.extcollectionsLMove(src, dst, from, to)
}

// extcollectionsLMove implements LMove assuming s.mu is already held.
func (s *Store) extcollectionsLMove(src, dst string, from, to ListEnd) (string, bool, error) {
	srcIt, err := s.listItem(src, false)
	if err != nil {
		return "", false, err
	}
	if srcIt == nil || len(srcIt.list) == 0 {
		return "", false, nil
	}
	if src != dst {
		if _, err := s.listItem(dst, false); err != nil {
			return "", false, err
		}
	}
	var val string
	if from == ListLeft {
		val = srcIt.list[0]
		srcIt.list = srcIt.list[1:]
	} else {
		val = srcIt.list[len(srcIt.list)-1]
		srcIt.list = srcIt.list[:len(srcIt.list)-1]
	}
	dstIt := srcIt
	if src != dst {
		dstIt, err = s.listItem(dst, true)
		if err != nil {
			return "", false, err
		}
	}
	if to == ListLeft {
		dstIt.list = append([]string{val}, dstIt.list...)
	} else {
		dstIt.list = append(dstIt.list, val)
	}
	if src != dst && len(srcIt.list) == 0 {
		delete(s.data, src)
	}
	return val, true, nil
}

// RPopLPush pops the tail element of the list at src and pushes it onto the head
// of the list at dst. It is equivalent to LMove(src, dst, ListRight, ListLeft).
func (s *Store) RPopLPush(src, dst string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.extcollectionsLMove(src, dst, ListRight, ListLeft)
}

// LTrim trims the list at key so that only the elements with indexes between
// start and stop, inclusive, remain. Negative indexes count from the tail.
// When the range selects nothing the key is deleted. It returns ErrWrongType if
// the key holds a non-list value.
func (s *Store) LTrim(key string, start, stop int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.listItem(key, false)
	if err != nil || it == nil {
		return err
	}
	lo, hi, ok := normalizeRange(start, stop, len(it.list))
	if !ok {
		delete(s.data, key)
		return nil
	}
	out := make([]string, hi-lo+1)
	copy(out, it.list[lo:hi+1])
	it.list = out
	return nil
}

// LPos returns the indexes of elements equal to val in the list at key. rank
// selects which match to start from: 1 is the first match, 2 the second, and a
// negative rank searches from the tail (-1 is the last match); rank 0 is treated
// as 1. count limits the number of indexes returned, with 0 meaning all
// matches. It returns ErrWrongType if the key holds a non-list value.
func (s *Store) LPos(key, val string, rank, count int) ([]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.listItem(key, false)
	if err != nil || it == nil {
		return []int{}, err
	}
	out := []int{}
	n := len(it.list)
	if rank == 0 {
		rank = 1
	}
	if rank > 0 {
		skip := rank - 1
		for i := 0; i < n; i++ {
			if it.list[i] == val {
				if skip > 0 {
					skip--
					continue
				}
				out = append(out, i)
				if count != 0 && len(out) >= count {
					break
				}
			}
		}
	} else {
		skip := -rank - 1
		for i := n - 1; i >= 0; i-- {
			if it.list[i] == val {
				if skip > 0 {
					skip--
					continue
				}
				out = append(out, i)
				if count != 0 && len(out) >= count {
					break
				}
			}
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Hash commands
// ---------------------------------------------------------------------------

// HIncrBy increments the integer value of field in the hash at key by delta and
// returns the result. A missing key or field is treated as 0. It returns
// ErrNotInteger if the field holds a non-integer and ErrWrongType if the key
// holds a non-hash value.
func (s *Store) HIncrBy(key, field string, delta int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.hashItem(key, true)
	if err != nil {
		return 0, err
	}
	cur := int64(0)
	if v, ok := it.hash[field]; ok {
		n, perr := strconv.ParseInt(v, 10, 64)
		if perr != nil {
			return 0, ErrNotInteger
		}
		cur = n
	}
	cur += delta
	it.hash[field] = strconv.FormatInt(cur, 10)
	return cur, nil
}

// HIncrByFloat increments the floating-point value of field in the hash at key
// by delta and returns the result. A missing key or field is treated as 0. The
// stored value is rewritten in Redis' human-readable float format (fixed-point,
// shortest round-trip). It returns ErrNotFloat if the field holds a non-float,
// ErrWrongType if the key holds a non-hash value, and ErrIncrNaNOrInf if the
// sum is NaN or infinite, in which case the field is left unchanged.
func (s *Store) HIncrByFloat(key, field string, delta float64) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.hashItem(key, true)
	if err != nil {
		return 0, err
	}
	cur := 0.0
	if v, ok := it.hash[field]; ok {
		f, perr := strconv.ParseFloat(v, 64)
		if perr != nil {
			return 0, ErrNotFloat
		}
		cur = f
	}
	cur += delta
	if math.IsNaN(cur) || math.IsInf(cur, 0) {
		return 0, ErrIncrNaNOrInf
	}
	it.hash[field] = formatFloatHuman(cur)
	return cur, nil
}

// HMGet returns the values of the given fields in the hash at key, in order. A
// nil entry marks a field that is absent; a missing key or a key holding a
// non-hash value yields all-nil entries.
func (s *Store) HMGet(key string, fields ...string) []*string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*string, len(fields))
	it := s.getLive(key)
	if it == nil || it.kind != TypeHash {
		return out
	}
	for i, f := range fields {
		if v, ok := it.hash[f]; ok {
			vv := v
			out[i] = &vv
		}
	}
	return out
}

// HSetNX sets field to val in the hash at key only if field does not already
// exist, reporting whether the write happened. It returns ErrWrongType if the
// key holds a non-hash value.
func (s *Store) HSetNX(key, field, val string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.hashItem(key, true)
	if err != nil {
		return false, err
	}
	if _, ok := it.hash[field]; ok {
		return false, nil
	}
	it.hash[field] = val
	return true, nil
}

// HRandField returns random field names from the hash at key. A positive count
// returns up to count distinct fields; a negative count returns exactly -count
// fields with repetition allowed. When withValues is true each field is followed
// by its value in the returned slice. It returns ErrWrongType if the key holds a
// non-hash value.
func (s *Store) HRandField(key string, count int, withValues bool) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.hashItem(key, false)
	if err != nil {
		return []string{}, err
	}
	if it == nil || len(it.hash) == 0 || count == 0 {
		return []string{}, nil
	}
	fields := extcollectionsHashFields(it.hash)
	var chosen []string
	if count < 0 {
		k := -count
		chosen = make([]string, 0, k)
		for i := 0; i < k; i++ {
			chosen = append(chosen, fields[rand.Intn(len(fields))])
		}
	} else {
		k := count
		if k > len(fields) {
			k = len(fields)
		}
		perm := rand.Perm(len(fields))
		chosen = make([]string, 0, k)
		for _, idx := range perm[:k] {
			chosen = append(chosen, fields[idx])
		}
	}
	if !withValues {
		return chosen, nil
	}
	out := make([]string, 0, len(chosen)*2)
	for _, f := range chosen {
		out = append(out, f, it.hash[f])
	}
	return out, nil
}

// HStrlen returns the length of the value of field in the hash at key, or 0 if
// the key or field is absent. It returns ErrWrongType if the key holds a
// non-hash value.
func (s *Store) HStrlen(key, field string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.hashItem(key, false)
	if err != nil || it == nil {
		return 0, err
	}
	return len(it.hash[field]), nil
}

// ---------------------------------------------------------------------------
// Set commands
// ---------------------------------------------------------------------------

// SPop removes and returns up to count random members from the set at key. A
// negative count is treated as zero. The key is deleted when it becomes empty.
// It returns ErrWrongType if the key holds a non-set value.
func (s *Store) SPop(key string, count int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.setItem(key, false)
	if err != nil || it == nil {
		return []string{}, err
	}
	if count <= 0 {
		return []string{}, nil
	}
	members := sortedKeys(it.set)
	k := count
	if k > len(members) {
		k = len(members)
	}
	perm := rand.Perm(len(members))
	chosen := make([]string, 0, k)
	for _, idx := range perm[:k] {
		chosen = append(chosen, members[idx])
	}
	for _, m := range chosen {
		delete(it.set, m)
	}
	if len(it.set) == 0 {
		delete(s.data, key)
	}
	return chosen, nil
}

// SRandMember returns random members from the set at key without removing them.
// A positive count returns up to count distinct members; a negative count
// returns exactly -count members with repetition allowed. It returns
// ErrWrongType if the key holds a non-set value.
func (s *Store) SRandMember(key string, count int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.setItem(key, false)
	if err != nil || it == nil {
		return []string{}, err
	}
	members := sortedKeys(it.set)
	if len(members) == 0 || count == 0 {
		return []string{}, nil
	}
	if count < 0 {
		k := -count
		out := make([]string, 0, k)
		for i := 0; i < k; i++ {
			out = append(out, members[rand.Intn(len(members))])
		}
		return out, nil
	}
	k := count
	if k > len(members) {
		k = len(members)
	}
	perm := rand.Perm(len(members))
	out := make([]string, 0, k)
	for _, idx := range perm[:k] {
		out = append(out, members[idx])
	}
	return out, nil
}

// SMove moves member from the set at src to the set at dst, which may equal src.
// It reports whether member was present in src and therefore moved. It returns
// ErrWrongType if either key holds a non-set value, and deletes src when it
// becomes empty.
func (s *Store) SMove(src, dst, member string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	srcIt, err := s.setItem(src, false)
	if err != nil {
		return false, err
	}
	if srcIt == nil {
		if _, err := s.setItem(dst, false); err != nil {
			return false, err
		}
		return false, nil
	}
	if _, ok := srcIt.set[member]; !ok {
		if src != dst {
			if _, err := s.setItem(dst, false); err != nil {
				return false, err
			}
		}
		return false, nil
	}
	dstIt, err := s.setItem(dst, true)
	if err != nil {
		return false, err
	}
	delete(srcIt.set, member)
	dstIt.set[member] = struct{}{}
	if src != dst && len(srcIt.set) == 0 {
		delete(s.data, src)
	}
	return true, nil
}

// SInterStore stores the intersection of the sets at keys into dst as a set and
// returns the cardinality of the result. An empty result deletes dst. It returns
// ErrWrongType if any source key holds a non-set value.
func (s *Store) SInterStore(dst string, keys ...string) (int, error) {
	members, err := s.SInter(keys...)
	if err != nil {
		return 0, err
	}
	return s.extcollectionsStoreSet(dst, members), nil
}

// SUnionStore stores the union of the sets at keys into dst as a set and returns
// the cardinality of the result. An empty result deletes dst. It returns
// ErrWrongType if any source key holds a non-set value.
func (s *Store) SUnionStore(dst string, keys ...string) (int, error) {
	members, err := s.SUnion(keys...)
	if err != nil {
		return 0, err
	}
	return s.extcollectionsStoreSet(dst, members), nil
}

// SDiffStore stores the difference of the sets at keys into dst as a set and
// returns the cardinality of the result. An empty result deletes dst. It returns
// ErrWrongType if any source key holds a non-set value.
func (s *Store) SDiffStore(dst string, keys ...string) (int, error) {
	members, err := s.SDiff(keys...)
	if err != nil {
		return 0, err
	}
	return s.extcollectionsStoreSet(dst, members), nil
}

// extcollectionsStoreSet writes members as a set value at dst, deleting dst when
// members is empty, and returns the stored cardinality.
func (s *Store) extcollectionsStoreSet(dst string, members []string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(members) == 0 {
		delete(s.data, dst)
		return 0
	}
	set := make(map[string]struct{}, len(members))
	for _, m := range members {
		set[m] = struct{}{}
	}
	s.data[dst] = &item{kind: TypeSet, set: set}
	return len(set)
}

// ---------------------------------------------------------------------------
// Sorted-set commands
// ---------------------------------------------------------------------------

// ZIncrBy increments the score of member in the sorted set at key by delta and
// returns the new score. A missing key or member starts from 0. It returns
// ErrWrongType if the key holds a non-zset value.
func (s *Store) ZIncrBy(key string, delta float64, member string) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, true)
	if err != nil {
		return 0, err
	}
	cur, _ := it.zset.score(member)
	newScore := cur + delta
	it.zset.add(member, newScore)
	return newScore, nil
}

// ZCount returns the number of members in the sorted set at key whose score
// falls within r. It returns ErrWrongType if the key holds a non-zset value.
func (s *Store) ZCount(key string, r ScoreRange) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return 0, err
	}
	n := 0
	for _, m := range it.zset.sl.toSlice() {
		if r.contains(m.Score) {
			n++
		}
	}
	return n, nil
}

// ZPopMin removes and returns up to count members with the lowest scores from
// the sorted set at key, ordered low-to-high. The key is deleted when it becomes
// empty. It returns ErrWrongType if the key holds a non-zset value.
func (s *Store) ZPopMin(key string, count int) ([]ZMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return []ZMember{}, err
	}
	all := it.zset.sl.toSlice()
	k := count
	if k < 0 {
		k = 0
	}
	if k > len(all) {
		k = len(all)
	}
	out := make([]ZMember, 0, k)
	for i := 0; i < k; i++ {
		out = append(out, all[i])
		it.zset.remove(all[i].Member)
	}
	if it.zset.len() == 0 {
		delete(s.data, key)
	}
	return out, nil
}

// ZPopMax removes and returns up to count members with the highest scores from
// the sorted set at key, ordered high-to-low. The key is deleted when it becomes
// empty. It returns ErrWrongType if the key holds a non-zset value.
func (s *Store) ZPopMax(key string, count int) ([]ZMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return []ZMember{}, err
	}
	all := it.zset.sl.toSlice()
	k := count
	if k < 0 {
		k = 0
	}
	if k > len(all) {
		k = len(all)
	}
	out := make([]ZMember, 0, k)
	for i := 0; i < k; i++ {
		m := all[len(all)-1-i]
		out = append(out, m)
		it.zset.remove(m.Member)
	}
	if it.zset.len() == 0 {
		delete(s.data, key)
	}
	return out, nil
}

// ZMScore returns the scores of the given members in the sorted set at key, in
// order. A nil entry marks a member that is absent; a missing key or a key
// holding a non-zset value yields all-nil entries.
func (s *Store) ZMScore(key string, members ...string) []*float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*float64, len(members))
	it := s.getLive(key)
	if it == nil || it.kind != TypeZSet {
		return out
	}
	for i, m := range members {
		if sc, ok := it.zset.score(m); ok {
			v := sc
			out[i] = &v
		}
	}
	return out
}

// LexRange bounds a ZRangeByLex query over members that share a common score.
// Min and Max are the string bounds; the Exclusive flags switch a bound from
// inclusive ("[") to exclusive ("("), and the Inf flags select the negative
// ("-") or positive ("+") infinity endpoints, ignoring the corresponding bound.
type LexRange struct {
	// Min is the lower member bound.
	Min string
	// Max is the upper member bound.
	Max string
	// MinExclusive excludes members equal to Min.
	MinExclusive bool
	// MaxExclusive excludes members equal to Max.
	MaxExclusive bool
	// MinInf selects the "-" endpoint, matching every member from the start.
	MinInf bool
	// MaxInf selects the "+" endpoint, matching every member to the end.
	MaxInf bool
}

// ZRangeByLex returns the members of the sorted set at key that fall within the
// lexical range r, in ascending member order. It assumes all members share the
// same score, as required by the Redis command. It returns ErrWrongType if the
// key holds a non-zset value.
func (s *Store) ZRangeByLex(key string, r LexRange) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return []string{}, err
	}
	out := []string{}
	for _, m := range it.zset.sl.toSlice() {
		if extcollectionsLexContains(r, m.Member) {
			out = append(out, m.Member)
		}
	}
	return out, nil
}

// ZRemRangeByRank removes the members of the sorted set at key whose rank is
// between start and stop, inclusive, ordered low-to-high score. Negative indexes
// count from the end. It returns the number removed and deletes the key when it
// becomes empty. It returns ErrWrongType if the key holds a non-zset value.
func (s *Store) ZRemRangeByRank(key string, start, stop int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return 0, err
	}
	all := it.zset.sl.toSlice()
	lo, hi, ok := normalizeRange(start, stop, len(all))
	if !ok {
		return 0, nil
	}
	for i := lo; i <= hi; i++ {
		it.zset.remove(all[i].Member)
	}
	if it.zset.len() == 0 {
		delete(s.data, key)
	}
	return hi - lo + 1, nil
}

// ZRemRangeByScore removes the members of the sorted set at key whose score
// falls within r. It returns the number removed and deletes the key when it
// becomes empty. It returns ErrWrongType if the key holds a non-zset value.
func (s *Store) ZRemRangeByScore(key string, r ScoreRange) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return 0, err
	}
	var toRemove []string
	for _, m := range it.zset.sl.toSlice() {
		if r.contains(m.Score) {
			toRemove = append(toRemove, m.Member)
		}
	}
	for _, m := range toRemove {
		it.zset.remove(m)
	}
	if it.zset.len() == 0 {
		delete(s.data, key)
	}
	return len(toRemove), nil
}

// ZStoreOp selects how scores are combined when a member appears in more than
// one source set during ZUnionStore and ZInterStore.
type ZStoreOp int

// Score aggregation modes for ZUnionStore and ZInterStore.
const (
	// ZStoreSum adds the weighted scores together.
	ZStoreSum ZStoreOp = iota
	// ZStoreMin keeps the smallest weighted score.
	ZStoreMin
	// ZStoreMax keeps the largest weighted score.
	ZStoreMax
)

// ZUnionStore stores the union of the sorted sets at keys into dst and returns
// the cardinality of the result. Each source score is multiplied by the matching
// entry in weights (nil means every weight is 1) before scores are combined with
// op. An empty result deletes dst. It returns ErrWrongType if any source key
// holds a non-zset value.
func (s *Store) ZUnionStore(dst string, keys []string, weights []float64, op ZStoreOp) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	weight := func(i int) float64 {
		if weights != nil && i < len(weights) {
			return weights[i]
		}
		return 1
	}
	result := newZSet()
	for i, key := range keys {
		it, err := s.zsetItem(key, false)
		if err != nil {
			return 0, err
		}
		if it == nil {
			continue
		}
		w := weight(i)
		for _, m := range it.zset.sl.toSlice() {
			val := m.Score * w
			if existing, ok := result.score(m.Member); ok {
				val = extcollectionsAggregate(op, existing, val)
			}
			result.add(m.Member, val)
		}
	}
	if result.len() == 0 {
		delete(s.data, dst)
		return 0, nil
	}
	s.data[dst] = &item{kind: TypeZSet, zset: result}
	return result.len(), nil
}

// ZInterStore stores the intersection of the sorted sets at keys into dst and
// returns the cardinality of the result. A member is included only if it is
// present in every source set. Each source score is multiplied by the matching
// entry in weights (nil means every weight is 1) before scores are combined with
// op. An empty result deletes dst. It returns ErrWrongType if any source key
// holds a non-zset value.
func (s *Store) ZInterStore(dst string, keys []string, weights []float64, op ZStoreOp) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	weight := func(i int) float64 {
		if weights != nil && i < len(weights) {
			return weights[i]
		}
		return 1
	}
	zsets := make([]*zset, len(keys))
	for i, key := range keys {
		it, err := s.zsetItem(key, false)
		if err != nil {
			return 0, err
		}
		if it == nil {
			delete(s.data, dst)
			return 0, nil
		}
		zsets[i] = it.zset
	}
	if len(zsets) == 0 {
		delete(s.data, dst)
		return 0, nil
	}
	result := newZSet()
	for _, m := range zsets[0].sl.toSlice() {
		agg := m.Score * weight(0)
		inAll := true
		for j := 1; j < len(zsets); j++ {
			sc, ok := zsets[j].score(m.Member)
			if !ok {
				inAll = false
				break
			}
			agg = extcollectionsAggregate(op, agg, sc*weight(j))
		}
		if inAll {
			result.add(m.Member, agg)
		}
	}
	if result.len() == 0 {
		delete(s.data, dst)
		return 0, nil
	}
	s.data[dst] = &item{kind: TypeZSet, zset: result}
	return result.len(), nil
}
