package redis

import "math/big"

// BitFieldType describes one integer field within a bitmap: its width in bits
// and whether it is interpreted as signed (two's complement) or unsigned. It
// mirrors the "u8", "i16", ... type encodings of the Redis BITFIELD command.
type BitFieldType struct {
	// Signed selects two's-complement interpretation when true.
	Signed bool
	// Bits is the field width. Unsigned fields allow 1..63 bits and signed
	// fields allow 1..64 bits, matching Redis.
	Bits uint
}

// valid reports whether the field width is within the range Redis accepts.
func (t BitFieldType) valid() bool {
	if t.Bits == 0 {
		return false
	}
	if t.Signed {
		return t.Bits <= 64
	}
	return t.Bits <= 63
}

// BitFieldOverflow selects how BitFieldIncrBy behaves when an increment pushes a
// field outside the range representable by its type.
type BitFieldOverflow int

// Overflow modes for BitFieldIncrBy, matching Redis BITFIELD OVERFLOW.
const (
	// OverflowWrap wraps around using two's-complement / modular arithmetic.
	OverflowWrap BitFieldOverflow = iota
	// OverflowSat saturates to the minimum or maximum value of the type.
	OverflowSat
	// OverflowFail leaves the field unchanged and reports failure.
	OverflowFail
)

// BitFieldGet returns the integer value of the field of type t whose first bit
// is at the given bit offset within the bitmap stored at key. Bits past the end
// of the value read as zero. A missing key reads as all zeros. It returns
// ErrSyntax for an invalid type and ErrWrongType if the key holds a non-string
// value. It mirrors the GET sub-operation of Redis BITFIELD.
func (s *Store) BitFieldGet(key string, t BitFieldType, offset uint) (int64, error) {
	if !t.valid() {
		return 0, ErrSyntax
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.bitfieldData(key)
	if err != nil {
		return 0, err
	}
	raw := bitfieldGetBits(data, offset, t.Bits)
	return bitfieldDecode(raw, t).Int64(), nil
}

// BitFieldSet sets the field of type t at the given bit offset within the
// bitmap stored at key to value, creating or growing the value as needed, and
// returns the field's previous value. It returns ErrSyntax for an invalid type
// and ErrWrongType if the key holds a non-string value. It mirrors the SET
// sub-operation of Redis BITFIELD.
func (s *Store) BitFieldSet(key string, t BitFieldType, offset uint, value int64) (int64, error) {
	if !t.valid() {
		return 0, ErrSyntax
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.bitfieldData(key)
	if err != nil {
		return 0, err
	}
	old := bitfieldDecode(bitfieldGetBits(data, offset, t.Bits), t).Int64()
	raw := bitfieldEncode(big.NewInt(value), t)
	data = bitfieldSetBits(data, offset, t.Bits, raw)
	s.data[key] = &item{kind: TypeString, str: string(data)}
	return old, nil
}

// BitFieldIncrBy adds incr to the field of type t at the given bit offset within
// the bitmap stored at key, applying the overflow policy ov, and returns the new
// value. The boolean is false only when ov is OverflowFail and the operation
// would overflow, in which case the field is left unchanged. It returns
// ErrSyntax for an invalid type and ErrWrongType if the key holds a non-string
// value. It mirrors the INCRBY sub-operation of Redis BITFIELD.
func (s *Store) BitFieldIncrBy(key string, t BitFieldType, offset uint, incr int64, ov BitFieldOverflow) (int64, bool, error) {
	if !t.valid() {
		return 0, false, ErrSyntax
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.bitfieldData(key)
	if err != nil {
		return 0, false, err
	}
	cur := bitfieldDecode(bitfieldGetBits(data, offset, t.Bits), t)
	sum := new(big.Int).Add(cur, big.NewInt(incr))
	res, ok := bitfieldApplyOverflow(sum, t, ov)
	if !ok {
		return 0, false, nil
	}
	data = bitfieldSetBits(data, offset, t.Bits, bitfieldEncode(res, t))
	s.data[key] = &item{kind: TypeString, str: string(data)}
	return res.Int64(), true, nil
}

// BitCountRange counts the set bits in the string stored at key between the
// inclusive offsets start and stop. When byBit is false the offsets are byte
// offsets (as in classic BITCOUNT); when true they are bit offsets (the BIT
// variant added in Redis 7.0). Negative offsets count back from the end. It
// returns ErrWrongType if the key holds a non-string value.
func (s *Store) BitCountRange(key string, start, stop int, byBit bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.bitfieldData(key)
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, nil
	}
	unit := len(data)
	if byBit {
		unit = len(data) * 8
	}
	lo, hi, ok := normalizeRange(start, stop, unit)
	if !ok {
		return 0, nil
	}
	count := 0
	if byBit {
		for bit := lo; bit <= hi; bit++ {
			if (data[bit/8]>>(7-uint(bit)%8))&1 == 1 {
				count++
			}
		}
	} else {
		for i := lo; i <= hi; i++ {
			b := data[i]
			for b != 0 {
				count += int(b & 1)
				b >>= 1
			}
		}
	}
	return count, nil
}

// bitfieldData returns a copy of the raw bytes of the string value at key.
// Callers must hold mu. A missing key yields an empty slice; a non-string value
// is an error.
func (s *Store) bitfieldData(key string) ([]byte, error) {
	it := s.getLive(key)
	if it == nil {
		return []byte{}, nil
	}
	if it.kind != TypeString {
		return nil, ErrWrongType
	}
	return []byte(it.str), nil
}

// bitfieldGetBits reads n bits starting at bit offset from data, big-endian
// within and across bytes. Bits beyond the slice read as zero.
func bitfieldGetBits(data []byte, offset, n uint) uint64 {
	var v uint64
	for i := uint(0); i < n; i++ {
		bit := offset + i
		byteIdx := bit / 8
		var b uint64
		if int(byteIdx) < len(data) {
			b = uint64((data[byteIdx] >> (7 - bit%8)) & 1)
		}
		v = (v << 1) | b
	}
	return v
}

// bitfieldSetBits writes the low n bits of value into data starting at bit
// offset, growing data with zero bytes as necessary, and returns the slice.
func bitfieldSetBits(data []byte, offset, n uint, value uint64) []byte {
	need := int((offset + n + 7) / 8)
	for len(data) < need {
		data = append(data, 0)
	}
	for i := uint(0); i < n; i++ {
		bit := offset + i
		byteIdx := bit / 8
		shift := 7 - bit%8
		if (value>>(n-1-i))&1 == 1 {
			data[byteIdx] |= 1 << shift
		} else {
			data[byteIdx] &^= 1 << shift
		}
	}
	return data
}

// bitfieldDecode interprets raw n-bit contents under type t as a signed or
// unsigned integer.
func bitfieldDecode(raw uint64, t BitFieldType) *big.Int {
	v := new(big.Int).SetUint64(raw)
	if t.Signed && t.Bits > 0 {
		// If the sign bit is set, subtract 2^bits.
		if raw&(uint64(1)<<(t.Bits-1)) != 0 {
			span := new(big.Int).Lsh(big.NewInt(1), t.Bits)
			v.Sub(v, span)
		}
	}
	return v
}

// bitfieldEncode reduces value into the n-bit two's-complement representation
// stored in the bitmap.
func bitfieldEncode(value *big.Int, t BitFieldType) uint64 {
	span := new(big.Int).Lsh(big.NewInt(1), t.Bits)
	m := new(big.Int).Mod(value, span) // Euclidean: result in [0, span)
	return m.Uint64()
}

// bitfieldRange returns the inclusive [min, max] representable by type t.
func bitfieldRange(t BitFieldType) (*big.Int, *big.Int) {
	if t.Signed {
		max := new(big.Int).Lsh(big.NewInt(1), t.Bits-1)
		max.Sub(max, big.NewInt(1))
		min := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), t.Bits-1))
		return min, max
	}
	max := new(big.Int).Lsh(big.NewInt(1), t.Bits)
	max.Sub(max, big.NewInt(1))
	return big.NewInt(0), max
}

// bitfieldApplyOverflow adjusts sum to fit type t under policy ov. ok is false
// only for OverflowFail when sum is out of range.
func bitfieldApplyOverflow(sum *big.Int, t BitFieldType, ov BitFieldOverflow) (*big.Int, bool) {
	min, max := bitfieldRange(t)
	if sum.Cmp(min) >= 0 && sum.Cmp(max) <= 0 {
		return sum, true
	}
	switch ov {
	case OverflowSat:
		if sum.Cmp(max) > 0 {
			return new(big.Int).Set(max), true
		}
		return new(big.Int).Set(min), true
	case OverflowFail:
		return nil, false
	default: // OverflowWrap
		span := new(big.Int).Lsh(big.NewInt(1), t.Bits)
		w := new(big.Int).Sub(sum, min)
		w.Mod(w, span)
		w.Add(w, min)
		return w, true
	}
}
