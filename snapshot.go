package redis

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"sort"
	"time"
)

// ErrBadSnapshot is returned when snapshot or dump bytes are malformed,
// truncated, or carry an unrecognized version.
var ErrBadSnapshot = errors.New("ERR bad snapshot payload")

// ErrBusyKey is returned by RestoreKey when the destination key already exists
// and replacement was not requested, matching the Redis BUSYKEY error.
var ErrBusyKey = errors.New("BUSYKEY Target key name already exists")

const (
	snapshotMagic   = "REDISNAP"
	snapshotVersion = 1
	dumpVersion     = 1

	snapKindString = 1
	snapKindList   = 2
	snapKindHash   = 3
	snapKindSet    = 4
	snapKindZSet   = 5
)

// MarshalSnapshot serializes the entire keyspace, including per-key expiration
// times, into a portable, deterministic byte slice. Keys and collection members
// are emitted in sorted order so that identical stores always produce identical
// bytes. Stream keys, which live outside the ordinary keyspace, are not
// included. The bytes can be reloaded with LoadSnapshot or NewFromSnapshot.
func (s *Store) MarshalSnapshot() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	keys := make([]string, 0, len(s.data))
	for k, it := range s.data {
		if it.hasTTL() && !s.now().Before(it.expireAt) {
			continue // skip already-expired keys
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	buf.WriteString(snapshotMagic)
	buf.WriteByte(snapshotVersion)
	snapshotPutU32(&buf, uint32(len(keys)))
	for _, k := range keys {
		it := s.data[k]
		snapshotPutStr(&buf, k)
		var exp int64
		if it.hasTTL() {
			exp = it.expireAt.UnixNano()
		}
		snapshotPutI64(&buf, exp)
		snapshotEncodeItem(&buf, it)
	}
	return buf.Bytes(), nil
}

// LoadSnapshot replaces the entire contents of the store with the keyspace
// encoded in data, which must have been produced by MarshalSnapshot. Expiration
// times are restored as absolute times, so they remain meaningful against the
// store's clock. It returns ErrBadSnapshot if data is malformed.
func (s *Store) LoadSnapshot(data []byte) error {
	r := &snapshotReader{b: data}
	if !r.expect(snapshotMagic) {
		return ErrBadSnapshot
	}
	ver, ok := r.readByte()
	if !ok || ver != snapshotVersion {
		return ErrBadSnapshot
	}
	n, ok := r.u32()
	if !ok {
		return ErrBadSnapshot
	}
	loaded := make(map[string]*item, n)
	for i := uint32(0); i < n; i++ {
		key, ok := r.str()
		if !ok {
			return ErrBadSnapshot
		}
		exp, ok := r.i64()
		if !ok {
			return ErrBadSnapshot
		}
		it, err := snapshotDecodeItem(r)
		if err != nil {
			return err
		}
		if exp != 0 {
			it.expireAt = time.Unix(0, exp)
		}
		loaded[key] = it
	}
	s.mu.Lock()
	s.data = loaded
	s.mu.Unlock()
	return nil
}

// NewFromSnapshot returns a new Store, backed by a real-time clock, populated
// from data as produced by MarshalSnapshot. It returns ErrBadSnapshot if data
// is malformed.
func NewFromSnapshot(data []byte) (*Store, error) {
	s := New()
	if err := s.LoadSnapshot(data); err != nil {
		return nil, err
	}
	return s, nil
}

// DumpKey serializes the value stored at key into a portable byte slice in the
// same spirit as the Redis DUMP command. The boolean is false when the key is
// absent or expired. The bytes carry the value but not its key or TTL and can be
// materialized into any store with RestoreKey.
func (s *Store) DumpKey(key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.getLive(key)
	if it == nil {
		return nil, false, nil
	}
	var buf bytes.Buffer
	buf.WriteByte(dumpVersion)
	snapshotEncodeItem(&buf, it)
	return buf.Bytes(), true, nil
}

// RestoreKey creates a value at key from data previously produced by DumpKey.
// If ttl is positive the key is given that time-to-live; a non-positive ttl
// leaves the key persistent. When the key already exists RestoreKey returns
// ErrBusyKey unless replace is true. It returns ErrBadSnapshot if data is
// malformed. It mirrors the Redis RESTORE command.
func (s *Store) RestoreKey(key string, data []byte, ttl time.Duration, replace bool) error {
	r := &snapshotReader{b: data}
	ver, ok := r.readByte()
	if !ok || ver != dumpVersion {
		return ErrBadSnapshot
	}
	it, err := snapshotDecodeItem(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !replace && s.getLive(key) != nil {
		return ErrBusyKey
	}
	if ttl > 0 {
		it.expireAt = s.now().Add(ttl)
	}
	s.data[key] = it
	return nil
}

// snapshotEncodeItem writes the kind byte and type-specific payload of it. It
// does not write the key or expiration.
func snapshotEncodeItem(buf *bytes.Buffer, it *item) {
	switch it.kind {
	case TypeString:
		buf.WriteByte(snapKindString)
		snapshotPutStr(buf, it.str)
	case TypeList:
		buf.WriteByte(snapKindList)
		snapshotPutU32(buf, uint32(len(it.list)))
		for _, e := range it.list {
			snapshotPutStr(buf, e)
		}
	case TypeHash:
		buf.WriteByte(snapKindHash)
		fields := make([]string, 0, len(it.hash))
		for f := range it.hash {
			fields = append(fields, f)
		}
		sort.Strings(fields)
		snapshotPutU32(buf, uint32(len(fields)))
		for _, f := range fields {
			snapshotPutStr(buf, f)
			snapshotPutStr(buf, it.hash[f])
		}
	case TypeSet:
		buf.WriteByte(snapKindSet)
		members := sortedKeys(it.set)
		snapshotPutU32(buf, uint32(len(members)))
		for _, m := range members {
			snapshotPutStr(buf, m)
		}
	case TypeZSet:
		buf.WriteByte(snapKindZSet)
		all := it.zset.sl.toSlice()
		snapshotPutU32(buf, uint32(len(all)))
		for _, m := range all {
			snapshotPutStr(buf, m.Member)
			snapshotPutF64(buf, m.Score)
		}
	}
}

// snapshotDecodeItem reads a kind byte and payload into a fresh item.
func snapshotDecodeItem(r *snapshotReader) (*item, error) {
	kind, ok := r.readByte()
	if !ok {
		return nil, ErrBadSnapshot
	}
	switch kind {
	case snapKindString:
		v, ok := r.str()
		if !ok {
			return nil, ErrBadSnapshot
		}
		return &item{kind: TypeString, str: v}, nil
	case snapKindList:
		n, ok := r.u32()
		if !ok {
			return nil, ErrBadSnapshot
		}
		list := make([]string, 0, n)
		for i := uint32(0); i < n; i++ {
			v, ok := r.str()
			if !ok {
				return nil, ErrBadSnapshot
			}
			list = append(list, v)
		}
		return &item{kind: TypeList, list: list}, nil
	case snapKindHash:
		n, ok := r.u32()
		if !ok {
			return nil, ErrBadSnapshot
		}
		h := make(map[string]string, n)
		for i := uint32(0); i < n; i++ {
			f, ok := r.str()
			if !ok {
				return nil, ErrBadSnapshot
			}
			v, ok := r.str()
			if !ok {
				return nil, ErrBadSnapshot
			}
			h[f] = v
		}
		return &item{kind: TypeHash, hash: h}, nil
	case snapKindSet:
		n, ok := r.u32()
		if !ok {
			return nil, ErrBadSnapshot
		}
		set := make(map[string]struct{}, n)
		for i := uint32(0); i < n; i++ {
			m, ok := r.str()
			if !ok {
				return nil, ErrBadSnapshot
			}
			set[m] = struct{}{}
		}
		return &item{kind: TypeSet, set: set}, nil
	case snapKindZSet:
		n, ok := r.u32()
		if !ok {
			return nil, ErrBadSnapshot
		}
		z := newZSet()
		for i := uint32(0); i < n; i++ {
			m, ok := r.str()
			if !ok {
				return nil, ErrBadSnapshot
			}
			sc, ok := r.f64()
			if !ok {
				return nil, ErrBadSnapshot
			}
			z.add(m, sc)
		}
		return &item{kind: TypeZSet, zset: z}, nil
	default:
		return nil, ErrBadSnapshot
	}
}

func snapshotPutU32(buf *bytes.Buffer, v uint32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	buf.Write(b[:])
}

func snapshotPutI64(buf *bytes.Buffer, v int64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(v))
	buf.Write(b[:])
}

func snapshotPutF64(buf *bytes.Buffer, v float64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], math.Float64bits(v))
	buf.Write(b[:])
}

func snapshotPutStr(buf *bytes.Buffer, s string) {
	snapshotPutU32(buf, uint32(len(s)))
	buf.WriteString(s)
}

// snapshotReader is a bounds-checked cursor over snapshot bytes.
type snapshotReader struct {
	b   []byte
	pos int
}

func (r *snapshotReader) expect(s string) bool {
	if r.pos+len(s) > len(r.b) {
		return false
	}
	if string(r.b[r.pos:r.pos+len(s)]) != s {
		return false
	}
	r.pos += len(s)
	return true
}

func (r *snapshotReader) readByte() (byte, bool) {
	if r.pos+1 > len(r.b) {
		return 0, false
	}
	v := r.b[r.pos]
	r.pos++
	return v, true
}

func (r *snapshotReader) u32() (uint32, bool) {
	if r.pos+4 > len(r.b) {
		return 0, false
	}
	v := binary.BigEndian.Uint32(r.b[r.pos:])
	r.pos += 4
	return v, true
}

func (r *snapshotReader) i64() (int64, bool) {
	if r.pos+8 > len(r.b) {
		return 0, false
	}
	v := binary.BigEndian.Uint64(r.b[r.pos:])
	r.pos += 8
	return int64(v), true
}

func (r *snapshotReader) f64() (float64, bool) {
	if r.pos+8 > len(r.b) {
		return 0, false
	}
	v := binary.BigEndian.Uint64(r.b[r.pos:])
	r.pos += 8
	return math.Float64frombits(v), true
}

func (r *snapshotReader) str() (string, bool) {
	n, ok := r.u32()
	if !ok {
		return "", false
	}
	if r.pos+int(n) > len(r.b) {
		return "", false
	}
	v := string(r.b[r.pos : r.pos+int(n)])
	r.pos += int(n)
	return v, true
}
