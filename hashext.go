package redis

import "math/rand"

// HashEntry is a single field/value pair of a hash, returned by
// HRandFieldWithValues.
type HashEntry struct {
	// Field is the field name.
	Field string
	// Value is the field's value.
	Value string
}

// HGetDel returns the values of the given fields in the hash at key and deletes
// those fields, all atomically. The result has one entry per requested field, in
// order; a nil entry marks a field that was absent. The key is deleted when its
// last field is removed. It returns ErrWrongType if the key holds a non-hash
// value, mirroring the Redis HGETDEL command.
func (s *Store) HGetDel(key string, fields ...string) ([]*string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*string, len(fields))
	it, err := s.hashItem(key, false)
	if err != nil {
		return nil, err
	}
	if it == nil {
		return out, nil
	}
	for i, f := range fields {
		if v, ok := it.hash[f]; ok {
			vv := v
			out[i] = &vv
			delete(it.hash, f)
		}
	}
	if len(it.hash) == 0 {
		delete(s.data, key)
	}
	return out, nil
}

// HRandFieldWithValues returns up to count random field/value pairs from the
// hash at key without removing them. A positive count returns distinct fields; a
// negative count returns exactly -count pairs with repetition allowed. A count
// of zero returns an empty slice. It returns ErrWrongType if the key holds a
// non-hash value. It is the typed WITHVALUES form of Redis HRANDFIELD.
func (s *Store) HRandFieldWithValues(key string, count int) ([]HashEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.hashItem(key, false)
	if err != nil || it == nil {
		return []HashEntry{}, err
	}
	fields := extcollectionsHashFields(it.hash)
	if len(fields) == 0 || count == 0 {
		return []HashEntry{}, nil
	}
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
	out := make([]HashEntry, 0, len(chosen))
	for _, f := range chosen {
		out = append(out, HashEntry{Field: f, Value: it.hash[f]})
	}
	return out, nil
}
