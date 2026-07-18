package redis

import "math/rand"

// ZLexCount returns the number of members in the sorted set at key that fall
// within the lexical range r. As with ZRangeByLex it assumes all members share
// the same score. It returns ErrWrongType if the key holds a non-zset value,
// mirroring the Redis ZLEXCOUNT command.
func (s *Store) ZLexCount(key string, r LexRange) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return 0, err
	}
	n := 0
	for _, m := range it.zset.sl.toSlice() {
		if extcollectionsLexContains(r, m.Member) {
			n++
		}
	}
	return n, nil
}

// ZRemRangeByLex removes the members of the sorted set at key that fall within
// the lexical range r, returning the number removed and deleting the key when it
// becomes empty. It assumes all members share the same score. It returns
// ErrWrongType if the key holds a non-zset value, mirroring the Redis
// ZREMRANGEBYLEX command.
func (s *Store) ZRemRangeByLex(key string, r LexRange) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return 0, err
	}
	var rm []string
	for _, m := range it.zset.sl.toSlice() {
		if extcollectionsLexContains(r, m.Member) {
			rm = append(rm, m.Member)
		}
	}
	for _, m := range rm {
		it.zset.remove(m)
	}
	if it.zset.len() == 0 {
		delete(s.data, key)
	}
	return len(rm), nil
}

// ZDiff returns the members of the sorted set at the first of keys that are not
// present in any of the remaining sorted sets, each carrying its score from the
// first set, ordered low-to-high. It returns ErrWrongType if any key holds a
// non-zset value, mirroring the Redis ZDIFF command (WITHSCORES form).
func (s *Store) ZDiff(keys ...string) ([]ZMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.zsetextDiff(keys)
	if err != nil {
		return nil, err
	}
	return result.sl.toSlice(), nil
}

// ZDiffStore stores the difference of the sorted set at the first of keys and
// the remaining sorted sets into dst, and returns the cardinality of the result.
// An empty result deletes dst. It returns ErrWrongType if any key holds a
// non-zset value, mirroring the Redis ZDIFFSTORE command.
func (s *Store) ZDiffStore(dst string, keys ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.zsetextDiff(keys)
	if err != nil {
		return 0, err
	}
	return s.zsetextStore(dst, result), nil
}

// ZUnion returns the union of the sorted sets at keys, ordered low-to-high by
// combined score. Each source score is multiplied by the matching entry in
// weights (nil means every weight is 1) before scores are combined with op. It
// returns ErrWrongType if any key holds a non-zset value, mirroring the Redis
// ZUNION command (WITHSCORES form).
func (s *Store) ZUnion(keys []string, weights []float64, op ZStoreOp) ([]ZMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.zsetextUnion(keys, weights, op)
	if err != nil {
		return nil, err
	}
	return result.sl.toSlice(), nil
}

// ZInter returns the intersection of the sorted sets at keys, ordered
// low-to-high by combined score. A member is included only if it is present in
// every source set. Each source score is multiplied by the matching entry in
// weights (nil means every weight is 1) before scores are combined with op. It
// returns ErrWrongType if any key holds a non-zset value, mirroring the Redis
// ZINTER command (WITHSCORES form).
func (s *Store) ZInter(keys []string, weights []float64, op ZStoreOp) ([]ZMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.zsetextInter(keys, weights, op)
	if err != nil {
		return nil, err
	}
	return result.sl.toSlice(), nil
}

// ZInterCard returns the number of members in the intersection of the sorted
// sets at keys. When limit is greater than zero the count stops once it reaches
// limit. It returns ErrWrongType if any key holds a non-zset value, mirroring
// the Redis ZINTERCARD command.
func (s *Store) ZInterCard(limit int, keys ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.zsetextInter(keys, nil, ZStoreSum)
	if err != nil {
		return 0, err
	}
	n := result.len()
	if limit > 0 && n > limit {
		n = limit
	}
	return n, nil
}

// ZRandMember returns random members from the sorted set at key without removing
// them. A positive count returns up to count distinct members; a negative count
// returns exactly -count members with repetition allowed. A count of zero
// returns an empty slice. It returns ErrWrongType if the key holds a non-zset
// value, mirroring the Redis ZRANDMEMBER command.
func (s *Store) ZRandMember(key string, count int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return []string{}, err
	}
	members := it.zset.sl.toSlice()
	if len(members) == 0 || count == 0 {
		return []string{}, nil
	}
	if count < 0 {
		k := -count
		out := make([]string, 0, k)
		for i := 0; i < k; i++ {
			out = append(out, members[rand.Intn(len(members))].Member)
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
		out = append(out, members[idx].Member)
	}
	return out, nil
}

// ZRangeStore stores into dst the members of the sorted set at src whose rank is
// between start and stop inclusive, and returns the cardinality of the result.
// When rev is true the ranks are taken in high-to-low score order. Negative
// indexes count from the end. An empty result deletes dst. It returns
// ErrWrongType if either key holds a non-zset value, mirroring the Redis
// ZRANGESTORE command by rank.
func (s *Store) ZRangeStore(dst, src string, start, stop int, rev bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(src, false)
	if err != nil {
		return 0, err
	}
	result := newZSet()
	if it != nil {
		all := it.zset.sl.toSlice()
		if rev {
			reverse(all)
		}
		if lo, hi, ok := normalizeRange(start, stop, len(all)); ok {
			for i := lo; i <= hi; i++ {
				result.add(all[i].Member, all[i].Score)
			}
		}
	}
	return s.zsetextStore(dst, result), nil
}

// ZMPopMin pops up to count members with the lowest scores from the first of
// keys that holds a non-empty sorted set, scanning keys left to right. It
// returns the key popped from and the popped members, ordered low-to-high. The
// boolean is false when none of the keys hold a non-empty sorted set. It returns
// ErrWrongType if a scanned key holds a non-zset value, mirroring Redis ZMPOP
// with the MIN modifier.
func (s *Store) ZMPopMin(count int, keys ...string) (string, []ZMember, bool, error) {
	return s.zsetextMPop(count, false, keys)
}

// ZMPopMax pops up to count members with the highest scores from the first of
// keys that holds a non-empty sorted set, scanning keys left to right. It
// returns the key popped from and the popped members, ordered high-to-low. The
// boolean is false when none of the keys hold a non-empty sorted set. It returns
// ErrWrongType if a scanned key holds a non-zset value, mirroring Redis ZMPOP
// with the MAX modifier.
func (s *Store) ZMPopMax(count int, keys ...string) (string, []ZMember, bool, error) {
	return s.zsetextMPop(count, true, keys)
}

// zsetextMPop implements ZMPopMin/ZMPopMax.
func (s *Store) zsetextMPop(count int, max bool, keys []string) (string, []ZMember, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range keys {
		it, err := s.zsetItem(key, false)
		if err != nil {
			return "", nil, false, err
		}
		if it == nil || it.zset.len() == 0 {
			continue
		}
		all := it.zset.sl.toSlice()
		if max {
			reverse(all)
		}
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
		return key, out, true, nil
	}
	return "", nil, false, nil
}

// zsetextStore writes result as a zset value at dst, deleting dst when result is
// empty, and returns the stored cardinality. Callers must hold mu.
func (s *Store) zsetextStore(dst string, result *zset) int {
	if result.len() == 0 {
		delete(s.data, dst)
		return 0
	}
	s.data[dst] = &item{kind: TypeZSet, zset: result}
	return result.len()
}

// zsetextWeight returns a weight lookup for the given weights slice, defaulting
// to 1 when weights is nil or too short.
func zsetextWeight(weights []float64) func(int) float64 {
	return func(i int) float64 {
		if weights != nil && i < len(weights) {
			return weights[i]
		}
		return 1
	}
}

// zsetextUnion computes the weighted union of the sorted sets at keys. Callers
// must hold mu.
func (s *Store) zsetextUnion(keys []string, weights []float64, op ZStoreOp) (*zset, error) {
	weight := zsetextWeight(weights)
	result := newZSet()
	for i, key := range keys {
		it, err := s.zsetItem(key, false)
		if err != nil {
			return nil, err
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
	return result, nil
}

// zsetextInter computes the weighted intersection of the sorted sets at keys.
// Callers must hold mu.
func (s *Store) zsetextInter(keys []string, weights []float64, op ZStoreOp) (*zset, error) {
	weight := zsetextWeight(weights)
	result := newZSet()
	if len(keys) == 0 {
		return result, nil
	}
	zsets := make([]*zset, len(keys))
	for i, key := range keys {
		it, err := s.zsetItem(key, false)
		if err != nil {
			return nil, err
		}
		if it == nil {
			return result, nil
		}
		zsets[i] = it.zset
	}
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
	return result, nil
}

// zsetextDiff computes the difference of the first sorted set at keys and the
// rest. Callers must hold mu.
func (s *Store) zsetextDiff(keys []string) (*zset, error) {
	result := newZSet()
	if len(keys) == 0 {
		return result, nil
	}
	first, err := s.zsetItem(keys[0], false)
	if err != nil {
		return nil, err
	}
	if first == nil {
		return result, nil
	}
	others := make([]*zset, 0, len(keys)-1)
	for _, key := range keys[1:] {
		it, err := s.zsetItem(key, false)
		if err != nil {
			return nil, err
		}
		if it != nil {
			others = append(others, it.zset)
		}
	}
	for _, m := range first.zset.sl.toSlice() {
		found := false
		for _, o := range others {
			if _, ok := o.score(m.Member); ok {
				found = true
				break
			}
		}
		if !found {
			result.add(m.Member, m.Score)
		}
	}
	return result, nil
}
