package redis

import "testing"

func TestBitFieldSetGet(t *testing.T) {
	s := New()
	u8 := BitFieldType{Signed: false, Bits: 8}
	i8 := BitFieldType{Signed: true, Bits: 8}

	// Setting a fresh field returns the previous (zero) value.
	old, err := s.BitFieldSet("bf", u8, 0, 255)
	if err != nil || old != 0 {
		t.Fatalf("BitFieldSet initial = %d, %v", old, err)
	}
	if got, _ := s.BitFieldGet("bf", u8, 0); got != 255 {
		t.Fatalf("u8 get = %d, want 255", got)
	}
	// The same bits read as -1 under a signed interpretation.
	if got, _ := s.BitFieldGet("bf", i8, 0); got != -1 {
		t.Fatalf("i8 get = %d, want -1", got)
	}

	// A second field at a non-zero offset is independent.
	if _, err := s.BitFieldSet("bf", u8, 8, 100); err != nil {
		t.Fatalf("BitFieldSet offset 8: %v", err)
	}
	if got, _ := s.BitFieldGet("bf", u8, 8); got != 100 {
		t.Fatalf("u8 get offset 8 = %d, want 100", got)
	}
	if got, _ := s.BitFieldGet("bf", u8, 0); got != 255 {
		t.Fatalf("first field disturbed: %d", got)
	}
}

func TestBitFieldIncrByOverflow(t *testing.T) {
	u8 := BitFieldType{Bits: 8}
	i8 := BitFieldType{Signed: true, Bits: 8}

	tests := []struct {
		name  string
		t     BitFieldType
		start int64
		incr  int64
		ov    BitFieldOverflow
		want  int64
		ok    bool
	}{
		{"wrap", u8, 255, 10, OverflowWrap, 9, true},
		{"sat-high", u8, 255, 10, OverflowSat, 255, true},
		{"fail-high", u8, 255, 10, OverflowFail, 0, false},
		{"sat-signed-high", i8, 120, 10, OverflowSat, 127, true},
		{"sat-signed-low", i8, -120, -20, OverflowSat, -128, true},
		{"wrap-signed", i8, 127, 1, OverflowWrap, -128, true},
		{"in-range", u8, 100, 20, OverflowFail, 120, true},
	}
	for _, tc := range tests {
		s := New()
		if _, err := s.BitFieldSet("k", tc.t, 0, tc.start); err != nil {
			t.Fatalf("%s: set: %v", tc.name, err)
		}
		got, ok, err := s.BitFieldIncrBy("k", tc.t, 0, tc.incr, tc.ov)
		if err != nil {
			t.Fatalf("%s: incr: %v", tc.name, err)
		}
		if ok != tc.ok || (ok && got != tc.want) {
			t.Fatalf("%s: got %d, ok=%v; want %d, ok=%v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestBitFieldInvalidType(t *testing.T) {
	s := New()
	if _, err := s.BitFieldGet("k", BitFieldType{Bits: 0}, 0); err != ErrSyntax {
		t.Fatalf("zero bits = %v, want ErrSyntax", err)
	}
	if _, err := s.BitFieldGet("k", BitFieldType{Bits: 64}, 0); err != ErrSyntax {
		t.Fatalf("unsigned 64 = %v, want ErrSyntax", err)
	}
}

func TestBitCountRange(t *testing.T) {
	s := New()
	s.Set("k", "foobar", SetOptions{})
	tests := []struct {
		start, stop int
		byBit       bool
		want        int
	}{
		{0, 0, false, 4},
		{1, 1, false, 6},
		{0, -1, false, 26},
		{5, 30, true, 17},
	}
	for _, tc := range tests {
		got, err := s.BitCountRange("k", tc.start, tc.stop, tc.byBit)
		if err != nil || got != tc.want {
			t.Fatalf("BitCountRange(%d,%d,%v) = %d, %v; want %d", tc.start, tc.stop, tc.byBit, got, err, tc.want)
		}
	}
}

func BenchmarkBitFieldIncrBy(b *testing.B) {
	s := New()
	u16 := BitFieldType{Bits: 16}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.BitFieldIncrBy("k", u16, 0, 1, OverflowWrap)
	}
}
