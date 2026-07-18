package redis

// ListDirection selects an end of a list for the multi-key pop command LMPop.
type ListDirection int

// List ends for LMPop, matching the LEFT and RIGHT keywords of Redis LMPOP.
const (
	// DirLeft selects the head of the list.
	DirLeft ListDirection = iota
	// DirRight selects the tail of the list.
	DirRight
)

// LPushX prepends values to the list at key only if key already holds a list,
// and returns the new length. If the key does not exist it makes no change and
// returns 0. It returns ErrWrongType if the key holds a non-list value,
// mirroring the Redis LPUSHX command.
func (s *Store) LPushX(key string, values ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.listItem(key, false)
	if err != nil {
		return 0, err
	}
	if it == nil {
		return 0, nil
	}
	for _, v := range values {
		it.list = append([]string{v}, it.list...)
	}
	return len(it.list), nil
}

// RPushX appends values to the list at key only if key already holds a list,
// and returns the new length. If the key does not exist it makes no change and
// returns 0. It returns ErrWrongType if the key holds a non-list value,
// mirroring the Redis RPUSHX command.
func (s *Store) RPushX(key string, values ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := s.listItem(key, false)
	if err != nil {
		return 0, err
	}
	if it == nil {
		return 0, nil
	}
	it.list = append(it.list, values...)
	return len(it.list), nil
}

// LPopN removes and returns up to count elements from the head of the list at
// key, in the order they are popped. A count of zero or less returns an empty
// slice. The key is deleted when it becomes empty. It returns ErrWrongType if
// the key holds a non-list value, mirroring the count form of Redis LPOP.
func (s *Store) LPopN(key string, count int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listsextPop(key, count, DirLeft)
}

// RPopN removes and returns up to count elements from the tail of the list at
// key, in the order they are popped. A count of zero or less returns an empty
// slice. The key is deleted when it becomes empty. It returns ErrWrongType if
// the key holds a non-list value, mirroring the count form of Redis RPOP.
func (s *Store) RPopN(key string, count int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listsextPop(key, count, DirRight)
}

// LMPop pops up to count elements from the given end of the first of keys that
// holds a non-empty list, scanning keys left to right. It returns the key that
// was popped from and the popped elements, in pop order. The boolean is false
// when none of the keys hold a non-empty list. It returns ErrWrongType if a
// scanned key holds a non-list value, mirroring the Redis LMPOP command.
func (s *Store) LMPop(dir ListDirection, count int, keys ...string) (string, []string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range keys {
		it, err := s.listItem(key, false)
		if err != nil {
			return "", nil, false, err
		}
		if it == nil || len(it.list) == 0 {
			continue
		}
		popped, err := s.listsextPop(key, count, dir)
		if err != nil {
			return "", nil, false, err
		}
		return key, popped, true, nil
	}
	return "", nil, false, nil
}

// listsextPop removes up to count elements from the given end of the list at
// key. Callers must hold mu.
func (s *Store) listsextPop(key string, count int, dir ListDirection) ([]string, error) {
	it, err := s.listItem(key, false)
	if err != nil || it == nil {
		return []string{}, err
	}
	if count < 0 {
		count = 0
	}
	if count > len(it.list) {
		count = len(it.list)
	}
	out := make([]string, 0, count)
	if dir == DirLeft {
		out = append(out, it.list[:count]...)
		it.list = it.list[count:]
	} else {
		for i := 0; i < count; i++ {
			out = append(out, it.list[len(it.list)-1-i])
		}
		it.list = it.list[:len(it.list)-count]
	}
	if len(it.list) == 0 {
		delete(s.data, key)
	}
	return out, nil
}
