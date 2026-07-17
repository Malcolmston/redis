package redis

import (
	"errors"
	"testing"
)

func TestSetBitAndGetBit(t *testing.T) {
	s := New()

	// Setting a bit on a fresh key returns the previous bit (0) and grows the
	// backing string as needed.
	if prev, err := s.SetBit("k", 7, 1); err != nil || prev != 0 {
		t.Fatalf("SetBit(k,7,1) = %d, %v; want 0, nil", prev, err)
	}
	// Bit 7 is the least-significant bit of the first byte, so the value is 0x01.
	if v, _, err := s.Get("k"); err != nil || v != "\x01" {
		t.Fatalf("Get after SetBit = %q, %v; want \"\\x01\", nil", v, err)
	}
	if bit, err := s.GetBit("k", 7); err != nil || bit != 1 {
		t.Fatalf("GetBit(k,7) = %d, %v; want 1, nil", bit, err)
	}
	// Overwriting an already-set bit returns the previous value.
	if prev, err := s.SetBit("k", 7, 0); err != nil || prev != 1 {
		t.Fatalf("SetBit(k,7,0) = %d, %v; want 1, nil", prev, err)
	}
	if bit, err := s.GetBit("k", 7); err != nil || bit != 0 {
		t.Fatalf("GetBit(k,7) after clear = %d, %v; want 0, nil", bit, err)
	}
	// Reading past the end yields 0.
	if bit, err := s.GetBit("k", 999); err != nil || bit != 0 {
		t.Fatalf("GetBit(k,999) = %d, %v; want 0, nil", bit, err)
	}
	// Missing key reads as 0.
	if bit, err := s.GetBit("missing", 3); err != nil || bit != 0 {
		t.Fatalf("GetBit(missing,3) = %d, %v; want 0, nil", bit, err)
	}
}

func TestSetBitGrowsAcrossBytes(t *testing.T) {
	s := New()
	// Offset 100 lives in byte 12 (100/8), so the string must be 13 bytes long.
	if _, err := s.SetBit("g", 100, 1); err != nil {
		t.Fatalf("SetBit(g,100,1) error: %v", err)
	}
	if n, err := s.Strlen("g"); err != nil || n != 13 {
		t.Fatalf("Strlen(g) = %d, %v; want 13, nil", n, err)
	}
	if bit, err := s.GetBit("g", 100); err != nil || bit != 1 {
		t.Fatalf("GetBit(g,100) = %d, %v; want 1, nil", bit, err)
	}
}

func TestSetBitErrors(t *testing.T) {
	s := New()
	if _, err := s.SetBit("k", 0, 2); !errors.Is(err, ErrSyntax) {
		t.Fatalf("SetBit value=2 err = %v; want ErrSyntax", err)
	}
	if _, err := s.SetBit("k", -1, 1); !errors.Is(err, ErrOutOfRange) {
		t.Fatalf("SetBit offset=-1 err = %v; want ErrOutOfRange", err)
	}

	// Wrong type is rejected for all bitmap operations.
	if _, err := s.LPush("list", "a"); err != nil {
		t.Fatalf("LPush setup error: %v", err)
	}
	if _, err := s.SetBit("list", 0, 1); !errors.Is(err, ErrWrongType) {
		t.Fatalf("SetBit on list err = %v; want ErrWrongType", err)
	}
	if _, err := s.GetBit("list", 0); !errors.Is(err, ErrWrongType) {
		t.Fatalf("GetBit on list err = %v; want ErrWrongType", err)
	}
	if _, err := s.BitCount("list", 0, -1, false); !errors.Is(err, ErrWrongType) {
		t.Fatalf("BitCount on list err = %v; want ErrWrongType", err)
	}
	if _, err := s.BitPos("list", 1, 0, -1, false); !errors.Is(err, ErrWrongType) {
		t.Fatalf("BitPos on list err = %v; want ErrWrongType", err)
	}
	if _, err := s.GetBit("k", -5); !errors.Is(err, ErrOutOfRange) {
		t.Fatalf("GetBit offset=-5 err = %v; want ErrOutOfRange", err)
	}
}

func TestBitCount(t *testing.T) {
	s := New()
	// "foobar" is the canonical Redis BITCOUNT example: 26 set bits total.
	s.Set("k", "foobar", SetOptions{})

	tests := []struct {
		name     string
		start    int64
		end      int64
		bitRange bool
		want     int64
	}{
		{"whole", 0, -1, false, 26},
		{"first-byte", 0, 0, false, 4},   // 'f' = 0x66 -> 4 bits
		{"second-byte", 1, 1, false, 6},  // 'o' = 0x6f -> 6 bits
		{"bytes-0-1", 0, 1, false, 10},   // f + o
		{"neg-range", -6, -1, false, 26}, // whole string via negatives
		{"empty-range", 5, 1, false, 0},  // start > end
		{"bit-range-0-5", 0, 5, true, 3}, // first 6 bits of 'f'=01100110 -> 0,1,1,0,0,1
		{"bit-range-0-0", 0, 0, true, 0}, // MSB of 'f' is 0
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.BitCount("k", tc.start, tc.end, tc.bitRange)
			if err != nil {
				t.Fatalf("BitCount error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("BitCount(%d,%d,bit=%v) = %d; want %d", tc.start, tc.end, tc.bitRange, got, tc.want)
			}
		})
	}

	// Missing key counts as 0.
	if got, err := s.BitCount("missing", 0, -1, false); err != nil || got != 0 {
		t.Fatalf("BitCount(missing) = %d, %v; want 0, nil", got, err)
	}
}

func TestBitPos(t *testing.T) {
	s := New()
	// 0x00 0xff 0xf0: first set bit is at bit index 8.
	s.Set("k", "\x00\xff\xf0", SetOptions{})

	tests := []struct {
		name        string
		bit         int
		start       int64
		end         int64
		useBitRange bool
		want        int64
	}{
		{"first-one", 1, 0, -1, false, 8},
		{"first-zero", 0, 0, -1, false, 0},
		{"one-from-byte-2", 1, 2, -1, false, 16}, // byte 2 = 0xf0, first one at bit 16
		{"zero-in-byte-1", 0, 1, 1, false, -1},   // byte 1 = 0xff, no zero
		{"bit-range-one", 1, 8, 23, true, 8},
		{"bit-range-zero", 0, 8, 15, true, -1}, // bits 8..15 are all one
		{"not-found-one", 1, 0, 0, false, -1},  // byte 0 = 0x00, no one
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.BitPos("k", tc.bit, tc.start, tc.end, tc.useBitRange)
			if err != nil {
				t.Fatalf("BitPos error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("BitPos(bit=%d,%d,%d,bit=%v) = %d; want %d", tc.bit, tc.start, tc.end, tc.useBitRange, got, tc.want)
			}
		})
	}

	if _, err := s.BitPos("k", 2, 0, -1, false); !errors.Is(err, ErrSyntax) {
		t.Fatalf("BitPos bit=2 err = %v; want ErrSyntax", err)
	}
	if got, err := s.BitPos("missing", 1, 0, -1, false); err != nil || got != -1 {
		t.Fatalf("BitPos(missing) = %d, %v; want -1, nil", got, err)
	}
}

func TestBitOp(t *testing.T) {
	s := New()
	s.Set("a", "\xff\x0f", SetOptions{})
	s.Set("b", "\x0f\xff", SetOptions{})
	s.Set("long", "\xff\xff\xff", SetOptions{})

	tests := []struct {
		name    string
		op      BitOp
		dest    string
		srcs    []string
		wantLen int64
		wantVal string
	}{
		{"and", BitOpAnd, "d1", []string{"a", "b"}, 2, "\x0f\x0f"},
		{"or", BitOpOr, "d2", []string{"a", "b"}, 2, "\xff\xff"},
		{"xor", BitOpXor, "d3", []string{"a", "b"}, 2, "\xf0\xf0"},
		{"not", BitOpNot, "d4", []string{"a"}, 2, "\x00\xf0"},
		// Zero-extension: longest source wins; shorter operand padded with 0.
		{"or-uneven", BitOpOr, "d5", []string{"a", "long"}, 3, "\xff\xff\xff"},
		{"and-uneven", BitOpAnd, "d6", []string{"a", "long"}, 3, "\xff\x0f\x00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n, err := s.BitOp(tc.op, tc.dest, tc.srcs...)
			if err != nil {
				t.Fatalf("BitOp error: %v", err)
			}
			if n != tc.wantLen {
				t.Fatalf("BitOp len = %d; want %d", n, tc.wantLen)
			}
			v, ok, err := s.Get(tc.dest)
			if err != nil || !ok {
				t.Fatalf("Get(%s) = %q, %v, %v", tc.dest, v, ok, err)
			}
			if v != tc.wantVal {
				t.Fatalf("BitOp result = %x; want %x", v, tc.wantVal)
			}
			if s.TypeOf(tc.dest) != TypeString {
				t.Fatalf("dest type = %s; want string", s.TypeOf(tc.dest))
			}
		})
	}
}

func TestBitOpErrorsAndEmpty(t *testing.T) {
	s := New()
	s.Set("a", "\xff", SetOptions{})

	// NOT requires exactly one source.
	if _, err := s.BitOp(BitOpNot, "d", "a", "a"); !errors.Is(err, ErrSyntax) {
		t.Fatalf("BitOp NOT two sources err = %v; want ErrSyntax", err)
	}
	// Binary op needs at least one source.
	if _, err := s.BitOp(BitOpAnd, "d"); !errors.Is(err, ErrWrongArgs) {
		t.Fatalf("BitOp AND no source err = %v; want ErrWrongArgs", err)
	}
	// Wrong type source.
	s.LPush("list", "x")
	if _, err := s.BitOp(BitOpOr, "d", "a", "list"); !errors.Is(err, ErrWrongType) {
		t.Fatalf("BitOp with list src err = %v; want ErrWrongType", err)
	}

	// Empty result deletes the destination key and returns 0.
	s.Set("d", "leftover", SetOptions{})
	n, err := s.BitOp(BitOpAnd, "d", "missing1", "missing2")
	if err != nil || n != 0 {
		t.Fatalf("BitOp empty result = %d, %v; want 0, nil", n, err)
	}
	if s.TypeOf("d") != TypeNone {
		t.Fatalf("dest still exists after empty BitOp: %s", s.TypeOf("d"))
	}
}

func TestBitmapDispatch(t *testing.T) {
	s := New()

	if _, err := s.Do("SETBIT", "k", "7", "1"); err != nil {
		t.Fatalf("Do SETBIT error: %v", err)
	}
	if v, err := s.Do("GETBIT", "k", "7"); err != nil || v.(int64) != 1 {
		t.Fatalf("Do GETBIT = %v, %v; want 1", v, err)
	}

	s.Set("c", "foobar", SetOptions{})
	if v, err := s.Do("BITCOUNT", "c"); err != nil || v.(int64) != 26 {
		t.Fatalf("Do BITCOUNT = %v, %v; want 26", v, err)
	}
	if v, err := s.Do("BITCOUNT", "c", "0", "0"); err != nil || v.(int64) != 4 {
		t.Fatalf("Do BITCOUNT range = %v, %v; want 4", v, err)
	}
	if v, err := s.Do("BITCOUNT", "c", "0", "5", "BIT"); err != nil || v.(int64) != 3 {
		t.Fatalf("Do BITCOUNT BIT = %v, %v; want 3", v, err)
	}

	s.Set("p", "\x00\xff", SetOptions{})
	if v, err := s.Do("BITPOS", "p", "1"); err != nil || v.(int64) != 8 {
		t.Fatalf("Do BITPOS = %v, %v; want 8", v, err)
	}

	s.Set("x", "\xff", SetOptions{})
	s.Set("y", "\x0f", SetOptions{})
	if v, err := s.Do("BITOP", "AND", "z", "x", "y"); err != nil || v.(int64) != 1 {
		t.Fatalf("Do BITOP = %v, %v; want 1", v, err)
	}
	if val, _, _ := s.Get("z"); val != "\x0f" {
		t.Fatalf("Do BITOP result = %x; want 0x0f", val)
	}
}
