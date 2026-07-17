package redis

import (
	"hash/fnv"
	"math"
)

// HyperLogLog support. Redis stores HyperLogLogs inside ordinary string
// values, and so does this package: an HLL is a TypeString item whose str
// begins with a fixed magic header followed by a dense array of one-byte
// registers. Keeping HLLs as strings means the existing string type checks
// (ErrWrongType) apply, while the magic header lets HLL commands tell an
// actual HLL apart from an unrelated string value.

const (
	// hllHeader is the 4-byte magic prefix that identifies a string value as
	// a HyperLogLog. It mirrors the constant Redis uses for the same purpose.
	hllHeader = "HYLL"
	// hllHeaderLen is the length in bytes of hllHeader.
	hllHeaderLen = 4
	// hllBits is the number of hash bits used to select a register; it fixes
	// the register count at 2^hllBits.
	hllBits = 14
	// hllRegisters is the number of dense registers in every HLL. With one
	// register stored per byte this keeps the encoding trivially simple.
	hllRegisters = 1 << hllBits
	// hllRemaining is the number of hash bits left after the register-index
	// bits are removed; the register rank is derived from these bits.
	hllRemaining = 64 - hllBits
	// hllDenseLen is the total byte length of a dense HLL value: the magic
	// header followed by one byte per register.
	hllDenseLen = hllHeaderLen + hllRegisters
)

// hllAlpha is the bias-correction constant for the harmonic-mean estimator,
// specialized for hllRegisters registers.
var hllAlpha = 0.7213 / (1.0 + 1.079/float64(hllRegisters))

// hllHash64 returns the FNV-1a 64-bit hash of elem. FNV is deterministic and
// depends only on the input bytes, which keeps HLL behavior reproducible.
func hllHash64(elem string) uint64 {
	h := fnv.New64a()
	// hash.Hash.Write never returns an error.
	_, _ = h.Write([]byte(elem))
	return h.Sum64()
}

// hllRank returns the register rank for the post-index hash bits w: the
// position (1-based) of the most significant set bit within the low
// hllRemaining bits, or hllRemaining+1 when no bit is set.
func hllRank(w uint64) uint8 {
	var rank uint8 = 1
	for mask := uint64(1) << (hllRemaining - 1); mask != 0; mask >>= 1 {
		if w&mask != 0 {
			break
		}
		rank++
	}
	return rank
}

// hllHashParts hashes elem and splits the digest into the register index (the
// low hllBits bits) and the rank derived from the remaining bits.
func hllHashParts(elem string) (index int, rank uint8) {
	h := hllHash64(elem)
	index = int(h & (hllRegisters - 1))
	rank = hllRank(h >> hllBits)
	return index, rank
}

// hllNewBytes returns a fresh dense HLL value: the magic header followed by
// hllRegisters zero registers.
func hllNewBytes() []byte {
	b := make([]byte, hllDenseLen)
	copy(b, hllHeader)
	return b
}

// hllValid reports whether str is a well-formed dense HLL value, i.e. it has
// the exact dense length and begins with the magic header.
func hllValid(str string) bool {
	return len(str) == hllDenseLen && str[:hllHeaderLen] == hllHeader
}

// hllApply folds elem into the register slice regs (which must have length
// hllRegisters) and reports whether a register was increased.
func hllApply(regs []byte, elem string) bool {
	index, rank := hllHashParts(elem)
	if rank > regs[index] {
		regs[index] = rank
		return true
	}
	return false
}

// hllEstimate returns the cardinality estimate for the register slice regs
// (length hllRegisters) using the standard harmonic-mean estimator with the
// small- and large-range bias corrections.
func hllEstimate(regs []byte) int64 {
	m := float64(hllRegisters)
	sum := 0.0
	zeros := 0
	for _, r := range regs {
		sum += math.Ldexp(1.0, -int(r)) // 2^-r
		if r == 0 {
			zeros++
		}
	}
	est := hllAlpha * m * m / sum

	const twoPow32 = 4294967296.0 // 2^32
	switch {
	case est <= 2.5*m && zeros != 0:
		// Small range: linear counting over the empty registers.
		est = m * math.Log(m/float64(zeros))
	case est > twoPow32/30.0:
		// Large range: correct for hash-space saturation.
		est = -twoPow32 * math.Log(1.0-est/twoPow32)
	}
	return int64(est + 0.5)
}

// PFAdd adds the given elements to the HyperLogLog stored at key, creating a
// new HLL there if the key is absent. It returns 1 if the HLL was created or
// at least one internal register was updated (so the cardinality estimate may
// have changed) and 0 otherwise. It returns ErrWrongType if key holds a value
// that is not a HyperLogLog string.
func (s *Store) PFAdd(key string, elements ...string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	it := s.getLive(key)
	if it == nil {
		b := hllNewBytes()
		regs := b[hllHeaderLen:]
		for _, e := range elements {
			hllApply(regs, e)
		}
		s.data[key] = &item{kind: TypeString, str: string(b)}
		return 1, nil
	}
	if it.kind != TypeString || !hllValid(it.str) {
		return 0, ErrWrongType
	}

	b := []byte(it.str)
	regs := b[hllHeaderLen:]
	updated := false
	for _, e := range elements {
		if hllApply(regs, e) {
			updated = true
		}
	}
	if updated {
		it.str = string(b)
		return 1, nil
	}
	return 0, nil
}

// PFCount returns the estimated cardinality of the HyperLogLog(s) at the given
// keys. With a single key it returns that key's estimate (0 if the key is
// absent). With multiple keys it estimates the cardinality of the union of
// their registers without persisting the merged result. It returns
// ErrWrongArgs if no key is given and ErrWrongType if any key holds a value
// that is not a HyperLogLog string.
func (s *Store) PFCount(keys ...string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(keys) == 0 {
		return 0, ErrWrongArgs
	}
	if len(keys) == 1 {
		it := s.getLive(keys[0])
		if it == nil {
			return 0, nil
		}
		if it.kind != TypeString || !hllValid(it.str) {
			return 0, ErrWrongType
		}
		return hllEstimate([]byte(it.str[hllHeaderLen:])), nil
	}

	merged := make([]byte, hllRegisters)
	for _, k := range keys {
		it := s.getLive(k)
		if it == nil {
			continue
		}
		if it.kind != TypeString || !hllValid(it.str) {
			return 0, ErrWrongType
		}
		src := it.str[hllHeaderLen:]
		for i := 0; i < hllRegisters; i++ {
			if src[i] > merged[i] {
				merged[i] = src[i]
			}
		}
	}
	return hllEstimate(merged), nil
}

// PFMerge writes to destKey the register-wise maximum of the HyperLogLogs at
// destKey (if any) and all srcKeys, producing a TypeString HLL representing the
// union of their observed elements. Absent keys are treated as empty HLLs. It
// returns ErrWrongType if destKey or any source key holds a value that is not a
// HyperLogLog string.
func (s *Store) PFMerge(destKey string, srcKeys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	merged := make([]byte, hllRegisters)
	fold := func(k string) error {
		it := s.getLive(k)
		if it == nil {
			return nil
		}
		if it.kind != TypeString || !hllValid(it.str) {
			return ErrWrongType
		}
		src := it.str[hllHeaderLen:]
		for i := 0; i < hllRegisters; i++ {
			if src[i] > merged[i] {
				merged[i] = src[i]
			}
		}
		return nil
	}

	if err := fold(destKey); err != nil {
		return err
	}
	for _, k := range srcKeys {
		if err := fold(k); err != nil {
			return err
		}
	}

	b := hllNewBytes()
	copy(b[hllHeaderLen:], merged)
	s.data[destKey] = &item{kind: TypeString, str: string(b)}
	return nil
}

// init wires the HyperLogLog commands into the shared dispatch table. It is
// nil-guarded so it is independent of package file initialization order.
func init() {
	if dispatchTable == nil {
		dispatchTable = make(map[string]handler)
	}
	dispatchTable["PFADD"] = hllCmdPFAdd
	dispatchTable["PFCOUNT"] = hllCmdPFCount
	dispatchTable["PFMERGE"] = hllCmdPFMerge
}

func hllCmdPFAdd(s *Store, a []string) (any, error) {
	if len(a) < 1 {
		return nil, ErrWrongArgs
	}
	n, err := s.PFAdd(a[0], a[1:]...)
	if err != nil {
		return nil, err
	}
	return int64(n), nil
}

func hllCmdPFCount(s *Store, a []string) (any, error) {
	if len(a) < 1 {
		return nil, ErrWrongArgs
	}
	n, err := s.PFCount(a...)
	if err != nil {
		return nil, err
	}
	return n, nil
}

func hllCmdPFMerge(s *Store, a []string) (any, error) {
	if len(a) < 1 {
		return nil, ErrWrongArgs
	}
	if err := s.PFMerge(a[0], a[1:]...); err != nil {
		return nil, err
	}
	return SimpleString("OK"), nil
}
