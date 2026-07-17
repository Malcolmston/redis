package redis

import "sort"

// hashItem returns the hash item at key, creating one if missing when create is
// true. It returns ErrWrongType for a non-hash value. Callers must hold mu.
func (s *Store) hashItem(key string, create bool) (*item, error) {
	it := s.getLive(key)
	if it == nil {
		if !create {
			return nil, nil
		}
		it = &item{kind: TypeHash, hash: make(map[string]string)}
		s.data[key] = it
		return it, nil
	}
	if it.kind != TypeHash {
		return nil, ErrWrongType
	}
	return it, nil
}

// HSet sets field-value pairs on the hash at key and returns the number of
// fields that were newly created (not counting updates to existing fields).
// pairs must have even length: field, value, field, value, ...
func (s *Store) HSet(key string, pairs ...string) (int, error) {
	if len(pairs)%2 != 0 {
		return 0, ErrWrongArgs
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.hashItem(key, true)
	if err != nil {
		return 0, err
	}
	added := 0
	for i := 0; i < len(pairs); i += 2 {
		if _, ok := it.hash[pairs[i]]; !ok {
			added++
		}
		it.hash[pairs[i]] = pairs[i+1]
	}
	return added, nil
}

// HGet returns the value of field in the hash at key. The boolean is false when
// the key or field is absent.
func (s *Store) HGet(key, field string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.hashItem(key, false)
	if err != nil || it == nil {
		return "", false, err
	}
	v, ok := it.hash[field]
	return v, ok, nil
}

// HDel removes the given fields from the hash at key and returns the number of
// fields removed. The key is deleted when its last field is removed.
func (s *Store) HDel(key string, fields ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.hashItem(key, false)
	if err != nil || it == nil {
		return 0, err
	}
	n := 0
	for _, f := range fields {
		if _, ok := it.hash[f]; ok {
			delete(it.hash, f)
			n++
		}
	}
	if len(it.hash) == 0 {
		delete(s.data, key)
	}
	return n, nil
}

// HExists reports whether field exists in the hash at key.
func (s *Store) HExists(key, field string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.hashItem(key, false)
	if err != nil || it == nil {
		return false, err
	}
	_, ok := it.hash[field]
	return ok, nil
}

// HLen returns the number of fields in the hash at key.
func (s *Store) HLen(key string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.hashItem(key, false)
	if err != nil || it == nil {
		return 0, err
	}
	return len(it.hash), nil
}

// HKeys returns the field names of the hash at key, sorted for determinism.
func (s *Store) HKeys(key string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.hashItem(key, false)
	if err != nil || it == nil {
		return []string{}, err
	}
	out := make([]string, 0, len(it.hash))
	for f := range it.hash {
		out = append(out, f)
	}
	sort.Strings(out)
	return out, nil
}

// HVals returns the values of the hash at key, ordered by sorted field name.
func (s *Store) HVals(key string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.hashItem(key, false)
	if err != nil || it == nil {
		return []string{}, err
	}
	fields := make([]string, 0, len(it.hash))
	for f := range it.hash {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = it.hash[f]
	}
	return out, nil
}

// HGetAll returns all field-value pairs of the hash at key as a map. The
// returned map is a copy and safe for the caller to mutate.
func (s *Store) HGetAll(key string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.hashItem(key, false)
	if err != nil || it == nil {
		return map[string]string{}, err
	}
	out := make(map[string]string, len(it.hash))
	for f, v := range it.hash {
		out[f] = v
	}
	return out, nil
}
