package redis

// ZAddOptions modifies the behavior of ZAddWith, mirroring the NX, XX, GT, LT,
// and CH flags of the Redis ZADD command. The zero value performs an
// unconditional add-or-update that counts only newly added members.
type ZAddOptions struct {
	// NX only adds new members and never updates the score of an existing one.
	NX bool
	// XX only updates existing members and never adds new ones.
	XX bool
	// GT only updates an existing member when the new score is greater than the
	// current score. New members are still added (unless XX is set).
	GT bool
	// LT only updates an existing member when the new score is less than the
	// current score. New members are still added (unless XX is set).
	LT bool
	// CH makes ZAddWith count changed members (added plus updated) rather than
	// only newly added members.
	CH bool
}

// ZAddWith adds or updates members in the sorted set at key subject to opts and
// returns the number of members added, or, when opts.CH is set, the number of
// members added or updated. It returns ErrSyntax for contradictory options
// (NX with any of XX/GT/LT, or GT with LT) and ErrWrongType if the key holds a
// non-zset value. It mirrors Redis ZADD with flags (excluding INCR, which is
// available through ZIncrBy).
func (s *Store) ZAddWith(key string, opts ZAddOptions, members ...ZMember) (int, error) {
	if opts.NX && (opts.XX || opts.GT || opts.LT) {
		return 0, ErrSyntax
	}
	if opts.GT && opts.LT {
		return 0, ErrSyntax
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.zsetItem(key, true)
	if err != nil {
		return 0, err
	}
	added, changed := 0, 0
	for _, m := range members {
		cur, exists := it.zset.score(m.Member)
		if exists {
			if opts.NX {
				continue
			}
			if opts.GT && !(m.Score > cur) {
				continue
			}
			if opts.LT && !(m.Score < cur) {
				continue
			}
			if m.Score != cur {
				it.zset.add(m.Member, m.Score)
				changed++
			}
		} else {
			if opts.XX {
				continue
			}
			it.zset.add(m.Member, m.Score)
			added++
		}
	}
	if it.zset.len() == 0 {
		delete(s.data, key)
	}
	if opts.CH {
		return added + changed, nil
	}
	return added, nil
}
