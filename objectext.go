package redis

// ObjectEncoding returns the internal encoding name of the value stored at key,
// such as "int", "embstr", "raw", "listpack", "hashtable", or "skiplist". The
// boolean is false when the key is missing or expired. It mirrors the Redis
// OBJECT ENCODING command.
func (s *Store) ObjectEncoding(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return "", false
	}
	return extkeyspaceEncoding(it), true
}

// ObjectRefCount returns the reference count of the value stored at key. This
// store keeps exactly one reference per key, so the count is always 1 for a live
// key. The boolean is false when the key is missing or expired. It mirrors the
// Redis OBJECT REFCOUNT command.
func (s *Store) ObjectRefCount(key string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getLive(key) == nil {
		return 0, false
	}
	return 1, true
}

// ObjectIdleTime returns the number of seconds the value at key has been idle.
// This store does not track access times, so the result is always 0 for a live
// key. The boolean is false when the key is missing or expired. It mirrors the
// Redis OBJECT IDLETIME command.
func (s *Store) ObjectIdleTime(key string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getLive(key) == nil {
		return 0, false
	}
	return 0, true
}

// ObjectFreq returns the logarithmic access frequency counter of the value at
// key. This store does not implement an LFU eviction policy, so the result is
// always 0 for a live key. The boolean is false when the key is missing or
// expired. It mirrors the Redis OBJECT FREQ command.
func (s *Store) ObjectFreq(key string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getLive(key) == nil {
		return 0, false
	}
	return 0, true
}

// MemoryUsage returns an estimate, in bytes, of the memory used by the value
// stored at key, including a fixed per-key overhead and the key name. The
// boolean is false when the key is missing or expired. The figure is a
// deterministic approximation, not the exact allocation, and mirrors the intent
// of the Redis MEMORY USAGE command.
func (s *Store) MemoryUsage(key string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return 0, false
	}
	return objectextMemory(key, it), true
}

// objectextItemOverhead is the fixed byte cost charged to every key by
// MemoryUsage, approximating the per-key bookkeeping of a real store.
const objectextItemOverhead = 16

// objectextMemory computes MemoryUsage's deterministic size estimate.
func objectextMemory(key string, it *item) int64 {
	size := int64(objectextItemOverhead + len(key))
	switch it.kind {
	case TypeString:
		size += int64(len(it.str))
	case TypeList:
		for _, e := range it.list {
			size += int64(len(e)) + 8
		}
	case TypeHash:
		for f, v := range it.hash {
			size += int64(len(f)+len(v)) + 8
		}
	case TypeSet:
		for m := range it.set {
			size += int64(len(m)) + 8
		}
	case TypeZSet:
		for _, m := range it.zset.sl.toSlice() {
			size += int64(len(m.Member)) + 16
		}
	}
	return size
}
