package redis

// listItem returns the list item at key, creating one if missing when create is
// true. It returns ErrWrongType if the key holds a non-list value. Callers must
// hold mu.
func (s *Store) listItem(key string, create bool) (*item, error) {
	it := s.getLive(key)
	if it == nil {
		if !create {
			return nil, nil
		}
		it = &item{kind: TypeList}
		s.data[key] = it
		return it, nil
	}
	if it.kind != TypeList {
		return nil, ErrWrongType
	}
	return it, nil
}

// LPush prepends values to the list at key (leftmost first) and returns the new
// length. Values are inserted so that the last argument ends up at the head,
// matching Redis LPUSH.
func (s *Store) LPush(key string, values ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.listItem(key, true)
	if err != nil {
		return 0, err
	}
	for _, v := range values {
		it.list = append([]string{v}, it.list...)
	}
	return len(it.list), nil
}

// RPush appends values to the list at key and returns the new length.
func (s *Store) RPush(key string, values ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.listItem(key, true)
	if err != nil {
		return 0, err
	}
	it.list = append(it.list, values...)
	return len(it.list), nil
}

// LPop removes and returns the head element of the list at key. The boolean is
// false when the list is empty or missing.
func (s *Store) LPop(key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.listItem(key, false)
	if err != nil || it == nil || len(it.list) == 0 {
		return "", false, err
	}
	v := it.list[0]
	it.list = it.list[1:]
	if len(it.list) == 0 {
		delete(s.data, key)
	}
	return v, true, nil
}

// RPop removes and returns the tail element of the list at key. The boolean is
// false when the list is empty or missing.
func (s *Store) RPop(key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.listItem(key, false)
	if err != nil || it == nil || len(it.list) == 0 {
		return "", false, err
	}
	v := it.list[len(it.list)-1]
	it.list = it.list[:len(it.list)-1]
	if len(it.list) == 0 {
		delete(s.data, key)
	}
	return v, true, nil
}

// LLen returns the length of the list at key, or 0 if absent.
func (s *Store) LLen(key string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.listItem(key, false)
	if err != nil || it == nil {
		return 0, err
	}
	return len(it.list), nil
}

// LIndex returns the element at index in the list at key. Negative indexes
// count from the tail (-1 is the last element). The boolean is false when the
// index is out of range or the key is absent.
func (s *Store) LIndex(key string, index int) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.listItem(key, false)
	if err != nil || it == nil {
		return "", false, err
	}
	i := index
	if i < 0 {
		i += len(it.list)
	}
	if i < 0 || i >= len(it.list) {
		return "", false, nil
	}
	return it.list[i], true, nil
}

// LRange returns the elements of the list at key between start and stop,
// inclusive. Negative indexes count from the tail. Out-of-range bounds are
// clamped, and an empty slice is returned when the range selects nothing.
func (s *Store) LRange(key string, start, stop int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.listItem(key, false)
	if err != nil || it == nil {
		return []string{}, err
	}
	lo, hi, ok := normalizeRange(start, stop, len(it.list))
	if !ok {
		return []string{}, nil
	}
	out := make([]string, hi-lo+1)
	copy(out, it.list[lo:hi+1])
	return out, nil
}

// normalizeRange converts possibly-negative inclusive [start, stop] indexes for
// a collection of length n into clamped bounds [lo, hi]. ok is false when the
// range is empty.
func normalizeRange(start, stop, n int) (lo, hi int, ok bool) {
	if n == 0 {
		return 0, 0, false
	}
	if start < 0 {
		start += n
	}
	if stop < 0 {
		stop += n
	}
	if start < 0 {
		start = 0
	}
	if stop >= n {
		stop = n - 1
	}
	if start > stop || start >= n || stop < 0 {
		return 0, 0, false
	}
	return start, stop, true
}
