package redis

import (
	"math/bits"
	"strconv"
	"strings"
)

// Bit operations treat a string value as a densely packed array of bits.
// Because Redis bitmaps are ordinary strings, these methods read and write the
// same underlying byte sequence exposed by the string commands. Within a byte,
// bit 0 is the most-significant bit, matching Redis addressing: the bit at
// offset o lives in byte o/8 at position 7-(o%8).

// BitOp identifies a bitwise operation performed by Store.BitOp over one or
// more source strings.
type BitOp int

// Recognized bitwise operations for Store.BitOp, mirroring the Redis BITOP
// operators. BitOpNot is unary; the others combine two or more sources.
const (
	// BitOpAnd computes the bitwise AND of all sources.
	BitOpAnd BitOp = iota
	// BitOpOr computes the bitwise OR of all sources.
	BitOpOr
	// BitOpXor computes the bitwise XOR of all sources.
	BitOpXor
	// BitOpNot computes the bitwise complement of a single source.
	BitOpNot
)

// strbitmapGetBitAt returns the bit (0 or 1) at absolute bit position pos in b,
// using most-significant-bit-first addressing. Positions outside b read as 0.
func strbitmapGetBitAt(b []byte, pos int64) int {
	if pos < 0 {
		return 0
	}
	byteIdx := pos / 8
	if byteIdx >= int64(len(b)) {
		return 0
	}
	shift := uint(7 - pos%8)
	return int((b[byteIdx] >> shift) & 1)
}

// strbitmapClampRange normalizes an inclusive [start, end] index range against a
// container of the given length. Negative indexes count back from the end. The
// result is clamped to [0, length-1]; ok is false when the range is empty or the
// container has zero length.
func strbitmapClampRange(start, end, length int64) (lo, hi int64, ok bool) {
	if length == 0 {
		return 0, 0, false
	}
	if start < 0 {
		start += length
	}
	if end < 0 {
		end += length
	}
	if start < 0 {
		start = 0
	}
	if end >= length {
		end = length - 1
	}
	if start > end || start >= length || end < 0 {
		return 0, 0, false
	}
	return start, end, true
}

// SetBit sets the bit at offset to value (which must be 0 or 1) and returns the
// previous bit stored there. The backing string is grown with zero bytes as
// needed so that offset becomes addressable. A missing key is created as an
// empty string. It returns ErrSyntax if value is not 0 or 1, ErrOutOfRange if
// offset is negative, and ErrWrongType if the key holds a non-string value.
func (s *Store) SetBit(key string, offset int64, value int) (int, error) {
	if value != 0 && value != 1 {
		return 0, ErrSyntax
	}
	if offset < 0 {
		return 0, ErrOutOfRange
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	it := s.getLive(key)
	if it != nil && it.kind != TypeString {
		return 0, ErrWrongType
	}

	byteIdx := offset / 8
	shift := uint(7 - offset%8)

	var b []byte
	if it != nil {
		b = []byte(it.str)
	}
	if int64(len(b)) <= byteIdx {
		grown := make([]byte, byteIdx+1)
		copy(grown, b)
		b = grown
	}

	prev := int((b[byteIdx] >> shift) & 1)
	if value == 1 {
		b[byteIdx] |= 1 << shift
	} else {
		b[byteIdx] &^= 1 << shift
	}

	if it != nil {
		it.str = string(b)
	} else {
		s.data[key] = &item{kind: TypeString, str: string(b)}
	}
	return prev, nil
}

// GetBit returns the bit stored at offset, or 0 when offset is past the end of
// the value or the key is missing. It returns ErrOutOfRange if offset is
// negative and ErrWrongType if the key holds a non-string value.
func (s *Store) GetBit(key string, offset int64) (int, error) {
	if offset < 0 {
		return 0, ErrOutOfRange
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	it := s.getLive(key)
	if it == nil {
		return 0, nil
	}
	if it.kind != TypeString {
		return 0, ErrWrongType
	}
	return strbitmapGetBitAt([]byte(it.str), offset), nil
}

// BitCount returns the number of set bits within an inclusive range of the
// value. When bitRange is false, start and end are byte indexes; when true they
// are bit indexes (the Redis BIT unit). Negative indexes count back from the
// end. A missing key counts as 0. It returns ErrWrongType if the key holds a
// non-string value.
func (s *Store) BitCount(key string, start, end int64, bitRange bool) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	it := s.getLive(key)
	if it == nil {
		return 0, nil
	}
	if it.kind != TypeString {
		return 0, ErrWrongType
	}
	b := []byte(it.str)

	if bitRange {
		lo, hi, ok := strbitmapClampRange(start, end, int64(len(b))*8)
		if !ok {
			return 0, nil
		}
		var count int64
		for pos := lo; pos <= hi; pos++ {
			count += int64(strbitmapGetBitAt(b, pos))
		}
		return count, nil
	}

	lo, hi, ok := strbitmapClampRange(start, end, int64(len(b)))
	if !ok {
		return 0, nil
	}
	var count int64
	for i := lo; i <= hi; i++ {
		count += int64(bits.OnesCount8(b[i]))
	}
	return count, nil
}

// BitPos returns the position of the first bit set to bit (0 or 1) within an
// inclusive range of the value, or -1 when no such bit is found. When
// useBitRange is false, start and end are byte indexes; when true they are bit
// indexes (the Redis BIT unit). Negative indexes count back from the end. A
// missing key yields -1. It returns ErrSyntax if bit is not 0 or 1 and
// ErrWrongType if the key holds a non-string value.
func (s *Store) BitPos(key string, bit int, start, end int64, useBitRange bool) (int64, error) {
	if bit != 0 && bit != 1 {
		return 0, ErrSyntax
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	it := s.getLive(key)
	if it == nil {
		return -1, nil
	}
	if it.kind != TypeString {
		return 0, ErrWrongType
	}
	b := []byte(it.str)

	var lo, hi int64
	var ok bool
	if useBitRange {
		lo, hi, ok = strbitmapClampRange(start, end, int64(len(b))*8)
	} else {
		var loByte, hiByte int64
		loByte, hiByte, ok = strbitmapClampRange(start, end, int64(len(b)))
		lo, hi = loByte*8, hiByte*8+7
	}
	if !ok {
		return -1, nil
	}
	for pos := lo; pos <= hi; pos++ {
		if strbitmapGetBitAt(b, pos) == bit {
			return pos, nil
		}
	}
	return -1, nil
}

// BitOp performs the bitwise operation op over the source keys and stores the
// result as a string at destKey, returning the length in bytes of the stored
// value. For BitOpAnd, BitOpOr and BitOpXor the result length equals the length
// of the longest source, with shorter operands zero-extended; missing sources
// are treated as empty. BitOpNot requires exactly one source and complements it.
// If the result is empty, destKey is deleted and 0 is returned. It returns
// ErrSyntax if BitOpNot is given other than one source or op is unknown,
// ErrWrongArgs if no source is given for a binary op, and ErrWrongType if any
// source holds a non-string value.
func (s *Store) BitOp(op BitOp, destKey string, srcKeys ...string) (int64, error) {
	if op == BitOpNot {
		if len(srcKeys) != 1 {
			return 0, ErrSyntax
		}
	} else if len(srcKeys) < 1 {
		return 0, ErrWrongArgs
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	srcs := make([][]byte, len(srcKeys))
	maxLen := 0
	for i, k := range srcKeys {
		it := s.getLive(k)
		if it == nil {
			continue
		}
		if it.kind != TypeString {
			return 0, ErrWrongType
		}
		srcs[i] = []byte(it.str)
		if len(srcs[i]) > maxLen {
			maxLen = len(srcs[i])
		}
	}

	var res []byte
	switch op {
	case BitOpNot:
		src := srcs[0]
		res = make([]byte, len(src))
		for i := range src {
			res[i] = ^src[i]
		}
	case BitOpAnd:
		res = make([]byte, maxLen)
		for i := 0; i < maxLen; i++ {
			acc := byte(0xFF)
			for _, src := range srcs {
				var v byte
				if i < len(src) {
					v = src[i]
				}
				acc &= v
			}
			res[i] = acc
		}
	case BitOpOr:
		res = make([]byte, maxLen)
		for i := 0; i < maxLen; i++ {
			var acc byte
			for _, src := range srcs {
				if i < len(src) {
					acc |= src[i]
				}
			}
			res[i] = acc
		}
	case BitOpXor:
		res = make([]byte, maxLen)
		for i := 0; i < maxLen; i++ {
			var acc byte
			for _, src := range srcs {
				if i < len(src) {
					acc ^= src[i]
				}
			}
			res[i] = acc
		}
	default:
		return 0, ErrSyntax
	}

	if len(res) == 0 {
		delete(s.data, destKey)
		return 0, nil
	}
	s.data[destKey] = &item{kind: TypeString, str: string(res)}
	return int64(len(res)), nil
}

// strbitmapCmdSetBit implements the SETBIT command.
func strbitmapCmdSetBit(s *Store, a []string) (any, error) {
	if len(a) != 3 {
		return nil, ErrWrongArgs
	}
	offset, err := strconv.ParseInt(a[1], 10, 64)
	if err != nil {
		return nil, ErrNotInteger
	}
	value, err := strconv.Atoi(a[2])
	if err != nil {
		return nil, ErrNotInteger
	}
	prev, err := s.SetBit(a[0], offset, value)
	if err != nil {
		return nil, err
	}
	return int64(prev), nil
}

// strbitmapCmdGetBit implements the GETBIT command.
func strbitmapCmdGetBit(s *Store, a []string) (any, error) {
	if len(a) != 2 {
		return nil, ErrWrongArgs
	}
	offset, err := strconv.ParseInt(a[1], 10, 64)
	if err != nil {
		return nil, ErrNotInteger
	}
	v, err := s.GetBit(a[0], offset)
	if err != nil {
		return nil, err
	}
	return int64(v), nil
}

// strbitmapCmdBitCount implements the BITCOUNT command, accepting an optional
// start/end range and an optional BYTE or BIT unit modifier.
func strbitmapCmdBitCount(s *Store, a []string) (any, error) {
	if len(a) != 1 && len(a) != 3 && len(a) != 4 {
		return nil, ErrWrongArgs
	}
	if len(a) == 1 {
		n, err := s.BitCount(a[0], 0, -1, false)
		if err != nil {
			return nil, err
		}
		return n, nil
	}
	start, err := strconv.ParseInt(a[1], 10, 64)
	if err != nil {
		return nil, ErrNotInteger
	}
	end, err := strconv.ParseInt(a[2], 10, 64)
	if err != nil {
		return nil, ErrNotInteger
	}
	bitRange := false
	if len(a) == 4 {
		switch strings.ToUpper(a[3]) {
		case "BYTE":
			bitRange = false
		case "BIT":
			bitRange = true
		default:
			return nil, ErrSyntax
		}
	}
	n, err := s.BitCount(a[0], start, end, bitRange)
	if err != nil {
		return nil, err
	}
	return n, nil
}

// strbitmapCmdBitPos implements the BITPOS command, accepting an optional start
// and end range and an optional BYTE or BIT unit modifier.
func strbitmapCmdBitPos(s *Store, a []string) (any, error) {
	if len(a) < 2 || len(a) > 5 {
		return nil, ErrWrongArgs
	}
	bit, err := strconv.Atoi(a[1])
	if err != nil {
		return nil, ErrNotInteger
	}
	var start int64
	end := int64(-1)
	if len(a) >= 3 {
		start, err = strconv.ParseInt(a[2], 10, 64)
		if err != nil {
			return nil, ErrNotInteger
		}
	}
	if len(a) >= 4 {
		end, err = strconv.ParseInt(a[3], 10, 64)
		if err != nil {
			return nil, ErrNotInteger
		}
	}
	useBitRange := false
	if len(a) == 5 {
		switch strings.ToUpper(a[4]) {
		case "BYTE":
			useBitRange = false
		case "BIT":
			useBitRange = true
		default:
			return nil, ErrSyntax
		}
	}
	n, err := s.BitPos(a[0], bit, start, end, useBitRange)
	if err != nil {
		return nil, err
	}
	return n, nil
}

// strbitmapCmdBitOp implements the BITOP command.
func strbitmapCmdBitOp(s *Store, a []string) (any, error) {
	if len(a) < 3 {
		return nil, ErrWrongArgs
	}
	var op BitOp
	switch strings.ToUpper(a[0]) {
	case "AND":
		op = BitOpAnd
	case "OR":
		op = BitOpOr
	case "XOR":
		op = BitOpXor
	case "NOT":
		op = BitOpNot
	default:
		return nil, ErrSyntax
	}
	n, err := s.BitOp(op, a[1], a[2:]...)
	if err != nil {
		return nil, err
	}
	return n, nil
}

// init registers the bitmap commands into the package dispatch table. This file
// sorts after dispatch.go, whose init builds the table; the nil guard keeps the
// registration robust regardless of initialization order.
func init() {
	if dispatchTable == nil {
		dispatchTable = map[string]handler{}
	}
	dispatchTable["SETBIT"] = strbitmapCmdSetBit
	dispatchTable["GETBIT"] = strbitmapCmdGetBit
	dispatchTable["BITCOUNT"] = strbitmapCmdBitCount
	dispatchTable["BITPOS"] = strbitmapCmdBitPos
	dispatchTable["BITOP"] = strbitmapCmdBitOp
}
