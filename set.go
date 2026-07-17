package redis

import "sort"

// setItem returns the set item at key, creating one if missing when create is
// true. It returns ErrWrongType for a non-set value. Callers must hold mu.
func (s *Store) setItem(key string, create bool) (*item, error) {
	it := s.getLive(key)
	if it == nil {
		if !create {
			return nil, nil
		}
		it = &item{kind: TypeSet, set: make(map[string]struct{})}
		s.data[key] = it
		return it, nil
	}
	if it.kind != TypeSet {
		return nil, ErrWrongType
	}
	return it, nil
}

// SAdd adds members to the set at key and returns the number of members that
// were newly added.
func (s *Store) SAdd(key string, members ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.setItem(key, true)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range members {
		if _, ok := it.set[m]; !ok {
			it.set[m] = struct{}{}
			n++
		}
	}
	return n, nil
}

// SRem removes members from the set at key and returns the number removed. The
// key is deleted when its last member is removed.
func (s *Store) SRem(key string, members ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.setItem(key, false)
	if err != nil || it == nil {
		return 0, err
	}
	n := 0
	for _, m := range members {
		if _, ok := it.set[m]; ok {
			delete(it.set, m)
			n++
		}
	}
	if len(it.set) == 0 {
		delete(s.data, key)
	}
	return n, nil
}

// SIsMember reports whether member is in the set at key.
func (s *Store) SIsMember(key, member string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.setItem(key, false)
	if err != nil || it == nil {
		return false, err
	}
	_, ok := it.set[member]
	return ok, nil
}

// SCard returns the number of members in the set at key.
func (s *Store) SCard(key string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.setItem(key, false)
	if err != nil || it == nil {
		return 0, err
	}
	return len(it.set), nil
}

// SMembers returns the members of the set at key, sorted for determinism.
func (s *Store) SMembers(key string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.setItem(key, false)
	if err != nil || it == nil {
		return []string{}, err
	}
	return sortedKeys(it.set), nil
}

// gatherSets collects the live set contents for keys. Callers must hold mu.
// It returns ErrWrongType if any key holds a non-set value.
func (s *Store) gatherSets(keys []string) ([]map[string]struct{}, error) {
	sets := make([]map[string]struct{}, 0, len(keys))
	for _, k := range keys {
		it, err := s.setItem(k, false)
		if err != nil {
			return nil, err
		}
		if it == nil {
			sets = append(sets, nil)
			continue
		}
		sets = append(sets, it.set)
	}
	return sets, nil
}

// SInter returns the intersection of the sets at keys, sorted.
func (s *Store) SInter(keys ...string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(keys) == 0 {
		return []string{}, nil
	}
	sets, err := s.gatherSets(keys)
	if err != nil {
		return nil, err
	}
	result := make(map[string]struct{})
	if sets[0] == nil {
		return []string{}, nil
	}
	for m := range sets[0] {
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
			result[m] = struct{}{}
		}
	}
	return sortedKeys(result), nil
}

// SUnion returns the union of the sets at keys, sorted.
func (s *Store) SUnion(keys ...string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sets, err := s.gatherSets(keys)
	if err != nil {
		return nil, err
	}
	result := make(map[string]struct{})
	for _, set := range sets {
		for m := range set {
			result[m] = struct{}{}
		}
	}
	return sortedKeys(result), nil
}

// SDiff returns the members of the first set at keys[0] that are not present in
// any of the subsequent sets, sorted.
func (s *Store) SDiff(keys ...string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(keys) == 0 {
		return []string{}, nil
	}
	sets, err := s.gatherSets(keys)
	if err != nil {
		return nil, err
	}
	result := make(map[string]struct{})
	if sets[0] == nil {
		return []string{}, nil
	}
	for m := range sets[0] {
		found := false
		for _, other := range sets[1:] {
			if other == nil {
				continue
			}
			if _, ok := other[m]; ok {
				found = true
				break
			}
		}
		if !found {
			result[m] = struct{}{}
		}
	}
	return sortedKeys(result), nil
}

// sortedKeys returns the keys of set in ascending sorted order.
func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for m := range set {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}
