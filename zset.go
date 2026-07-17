package redis

import "math"

// zsetItem returns the sorted-set item at key, creating one if missing when
// create is true. It returns ErrWrongType for a non-zset value. Callers must
// hold mu.
func (s *Store) zsetItem(key string, create bool) (*item, error) {
	it := s.getLive(key)
	if it == nil {
		if !create {
			return nil, nil
		}
		it = &item{kind: TypeZSet, zset: newZSet()}
		s.data[key] = it
		return it, nil
	}
	if it.kind != TypeZSet {
		return nil, ErrWrongType
	}
	return it, nil
}

// ZMember pairs a member with its score, used by score-returning range queries.
type ZMember = zmember

// ZAdd adds or updates members with the given scores and returns the number of
// members newly added (updates to existing members are not counted).
func (s *Store) ZAdd(key string, members ...ZMember) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, true)
	if err != nil {
		return 0, err
	}
	added := 0
	for _, m := range members {
		if it.zset.add(m.Member, m.Score) {
			added++
		}
	}
	return added, nil
}

// ZRem removes members from the sorted set at key and returns the number
// removed. The key is deleted when it becomes empty.
func (s *Store) ZRem(key string, members ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return 0, err
	}
	n := 0
	for _, m := range members {
		if it.zset.remove(m) {
			n++
		}
	}
	if it.zset.len() == 0 {
		delete(s.data, key)
	}
	return n, nil
}

// ZScore returns the score of member in the sorted set at key. The boolean is
// false when the key or member is absent.
func (s *Store) ZScore(key, member string) (float64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return 0, false, err
	}
	sc, ok := it.zset.score(member)
	return sc, ok, nil
}

// ZCard returns the number of members in the sorted set at key.
func (s *Store) ZCard(key string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return 0, err
	}
	return it.zset.len(), nil
}

// ZRank returns the zero-based rank of member ordered from lowest score to
// highest. The boolean is false when the key or member is absent.
func (s *Store) ZRank(key, member string) (int, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return 0, false, err
	}
	r, ok := it.zset.rank(member)
	return r, ok, nil
}

// ZRevRank returns the zero-based rank of member ordered from highest score to
// lowest.
func (s *Store) ZRevRank(key, member string) (int, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return 0, false, err
	}
	r, ok := it.zset.rank(member)
	if !ok {
		return 0, false, nil
	}
	return it.zset.len() - 1 - r, true, nil
}

// ZRange returns members with rank between start and stop inclusive, ordered
// low-to-high score. Negative indexes count from the end. If withScores is
// true, the returned members carry their scores; otherwise Score is zero.
func (s *Store) ZRange(key string, start, stop int) ([]ZMember, error) {
	return s.zrange(key, start, stop, false)
}

// ZRevRange is ZRange with high-to-low score ordering.
func (s *Store) ZRevRange(key string, start, stop int) ([]ZMember, error) {
	return s.zrange(key, start, stop, true)
}

func (s *Store) zrange(key string, start, stop int, rev bool) ([]ZMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return []ZMember{}, err
	}
	all := it.zset.sl.toSlice()
	if rev {
		reverse(all)
	}
	lo, hi, ok := normalizeRange(start, stop, len(all))
	if !ok {
		return []ZMember{}, nil
	}
	out := make([]ZMember, hi-lo+1)
	copy(out, all[lo:hi+1])
	return out, nil
}

// ScoreRange bounds a ZRangeByScore query. Use math.Inf for open ends.
type ScoreRange struct {
	// Min is the lower score bound.
	Min float64
	// Max is the upper score bound.
	Max float64
	// MinExclusive excludes members whose score equals Min.
	MinExclusive bool
	// MaxExclusive excludes members whose score equals Max.
	MaxExclusive bool
}

// ZRangeByScore returns members whose score falls within r, ordered
// low-to-high. Members with equal scores are ordered lexicographically.
func (s *Store) ZRangeByScore(key string, r ScoreRange) ([]ZMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return []ZMember{}, err
	}
	out := make([]ZMember, 0)
	for _, m := range it.zset.sl.toSlice() {
		if r.contains(m.Score) {
			out = append(out, m)
		}
	}
	return out, nil
}

func (r ScoreRange) contains(score float64) bool {
	lo := r.Min
	hi := r.Max
	if math.IsNaN(score) {
		return false
	}
	if r.MinExclusive {
		if !(score > lo) {
			return false
		}
	} else if score < lo {
		return false
	}
	if r.MaxExclusive {
		if !(score < hi) {
			return false
		}
	} else if score > hi {
		return false
	}
	return true
}

func reverse(m []zmember) {
	for i, j := 0, len(m)-1; i < j; i, j = i+1, j-1 {
		m[i], m[j] = m[j], m[i]
	}
}
