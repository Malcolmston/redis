package redis

// SMIsMember reports, for each member, whether it belongs to the set at key,
// returning a slice of booleans in the same order as members. A missing key
// yields all-false results. It returns ErrWrongType if the key holds a non-set
// value, mirroring the Redis SMISMEMBER command.
func (s *Store) SMIsMember(key string, members ...string) ([]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]bool, len(members))
	it, err := s.setItem(key, false)
	if err != nil {
		return nil, err
	}
	if it == nil {
		return out, nil
	}
	for i, m := range members {
		_, out[i] = it.set[m]
	}
	return out, nil
}

// SInterCard returns the number of members in the intersection of the sets at
// keys. When limit is greater than zero the count stops as soon as it reaches
// limit, which bounds the work performed on large sets. It returns ErrWrongType
// if any key holds a non-set value, mirroring the Redis SINTERCARD command.
func (s *Store) SInterCard(limit int, keys ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(keys) == 0 {
		return 0, nil
	}
	sets, err := s.gatherSets(keys)
	if err != nil {
		return 0, err
	}
	if sets[0] == nil {
		return 0, nil
	}
	// Iterate the smallest set for efficiency, but sort for determinism.
	base := sortedKeys(sets[0])
	count := 0
	for _, m := range base {
		inAll := true
		for _, other := range sets[1:] {
			if other == nil {
				inAll = false
				break
			}
			if _, ok := other[m]; !ok {
				inAll = false
				break
			}
		}
		if inAll {
			count++
			if limit > 0 && count >= limit {
				break
			}
		}
	}
	return count, nil
}
