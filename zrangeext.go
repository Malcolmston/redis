package redis

import "math/rand"

// ZRevRangeByScore returns the members of the sorted set at key whose score
// falls within r, ordered high-to-low (members with equal scores are ordered in
// reverse lexical order). It returns ErrWrongType if the key holds a non-zset
// value, mirroring the Redis ZREVRANGEBYSCORE command.
func (s *Store) ZRevRangeByScore(key string, r ScoreRange) ([]ZMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.zrangeextByScore(key, r, true, 0, -1)
	return out, err
}

// ZRangeByScoreLimit is ZRangeByScore restricted to a window: it skips the
// first offset matching members and returns at most count of the rest. A count
// less than zero returns every member after the offset. It mirrors the LIMIT
// modifier of Redis ZRANGEBYSCORE. It returns ErrWrongType if the key holds a
// non-zset value.
func (s *Store) ZRangeByScoreLimit(key string, r ScoreRange, offset, count int) ([]ZMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.zrangeextByScore(key, r, false, offset, count)
}

// ZRevRangeByScoreLimit is ZRevRangeByScore restricted to a window: it skips the
// first offset matching members (in high-to-low order) and returns at most
// count of the rest. A count less than zero returns every member after the
// offset. It mirrors the LIMIT modifier of Redis ZREVRANGEBYSCORE. It returns
// ErrWrongType if the key holds a non-zset value.
func (s *Store) ZRevRangeByScoreLimit(key string, r ScoreRange, offset, count int) ([]ZMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.zrangeextByScore(key, r, true, offset, count)
}

// zrangeextByScore gathers members matching r, optionally reversed, then applies
// the offset/count window. A count below zero means unbounded. Callers must hold
// mu.
func (s *Store) zrangeextByScore(key string, r ScoreRange, rev bool, offset, count int) ([]ZMember, error) {
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return []ZMember{}, err
	}
	all := it.zset.sl.toSlice()
	matched := make([]ZMember, 0)
	for _, m := range all {
		if r.contains(m.Score) {
			matched = append(matched, m)
		}
	}
	if rev {
		reverse(matched)
	}
	return zrangeextWindow(matched, offset, count), nil
}

// ZRevRangeByLex returns the members of the sorted set at key that fall within
// the lexical range r, in descending member order. It assumes all members share
// the same score. It returns ErrWrongType if the key holds a non-zset value,
// mirroring the Redis ZREVRANGEBYLEX command.
func (s *Store) ZRevRangeByLex(key string, r LexRange) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.zrangeextByLex(key, r, true, 0, -1)
	return out, err
}

// ZRangeByLexLimit is ZRangeByLex restricted to a window: it skips the first
// offset matching members and returns at most count of the rest. A count less
// than zero returns every member after the offset. It mirrors the LIMIT
// modifier of Redis ZRANGEBYLEX. It returns ErrWrongType if the key holds a
// non-zset value.
func (s *Store) ZRangeByLexLimit(key string, r LexRange, offset, count int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.zrangeextByLex(key, r, false, offset, count)
}

// zrangeextByLex gathers members matching the lexical range r, optionally
// reversed, then applies the offset/count window. Callers must hold mu.
func (s *Store) zrangeextByLex(key string, r LexRange, rev bool, offset, count int) ([]string, error) {
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return []string{}, err
	}
	matched := make([]string, 0)
	for _, m := range it.zset.sl.toSlice() {
		if extcollectionsLexContains(r, m.Member) {
			matched = append(matched, m.Member)
		}
	}
	if rev {
		for l, rr := 0, len(matched)-1; l < rr; l, rr = l+1, rr-1 {
			matched[l], matched[rr] = matched[rr], matched[l]
		}
	}
	return zrangeextStrWindow(matched, offset, count), nil
}

// ZRandMemberWithScores returns up to count random members of the sorted set at
// key together with their scores, without removing them. A positive count
// returns distinct members; a negative count returns exactly -count members with
// repetition allowed. It returns ErrWrongType if the key holds a non-zset value,
// mirroring Redis ZRANDMEMBER ... WITHSCORES.
func (s *Store) ZRandMemberWithScores(key string, count int) ([]ZMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, false)
	if err != nil || it == nil {
		return []ZMember{}, err
	}
	members := it.zset.sl.toSlice()
	if len(members) == 0 || count == 0 {
		return []ZMember{}, nil
	}
	if count < 0 {
		k := -count
		out := make([]ZMember, 0, k)
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
	out := make([]ZMember, 0, k)
	for _, idx := range perm[:k] {
		out = append(out, members[idx])
	}
	return out, nil
}

// zrangeextWindow applies an offset/count window to a slice of members. A
// negative count means take everything after the offset.
func zrangeextWindow(in []ZMember, offset, count int) []ZMember {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(in) {
		return []ZMember{}
	}
	in = in[offset:]
	if count >= 0 && count < len(in) {
		in = in[:count]
	}
	out := make([]ZMember, len(in))
	copy(out, in)
	return out
}

// zrangeextStrWindow applies an offset/count window to a slice of strings.
func zrangeextStrWindow(in []string, offset, count int) []string {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(in) {
		return []string{}
	}
	in = in[offset:]
	if count >= 0 && count < len(in) {
		in = in[:count]
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}
