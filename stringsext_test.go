package redis

import (
	"reflect"
	"testing"
	"time"
)

func TestGetDel(t *testing.T) {
	s := New()
	s.Set("k", "v", SetOptions{})
	v, ok, err := s.GetDel("k")
	if err != nil || !ok || v != "v" {
		t.Fatalf("GetDel = %q, %v, %v", v, ok, err)
	}
	if s.Exists("k") != 0 {
		t.Fatalf("key not deleted")
	}
	if _, ok, _ := s.GetDel("missing"); ok {
		t.Fatalf("GetDel of missing key returned ok")
	}
	s.RPush("l", "a")
	if _, _, err := s.GetDel("l"); err != ErrWrongType {
		t.Fatalf("GetDel on list = %v, want ErrWrongType", err)
	}
}

func TestGetEx(t *testing.T) {
	clk := NewManualClock(time.Unix(0, 0))
	s := NewWithClock(clk)
	s.Set("k", "v", SetOptions{EX: 100 * time.Second})

	// Persist clears the TTL.
	if _, ok, err := s.GetEx("k", GetExOptions{Persist: true}); !ok || err != nil {
		t.Fatalf("GetEx persist = %v, %v", ok, err)
	}
	if _, code := s.TTL("k"); code != TTLNoExpiry {
		t.Fatalf("TTL after persist = %v, want TTLNoExpiry", code)
	}

	// EX sets a fresh TTL.
	if _, _, err := s.GetEx("k", GetExOptions{EX: 10 * time.Second}); err != nil {
		t.Fatalf("GetEx EX: %v", err)
	}
	clk.Advance(11 * time.Second)
	if _, ok, _ := s.Get("k"); ok {
		t.Fatalf("key should have expired")
	}

	// No options leaves state unchanged (and key is gone now).
	if _, ok, _ := s.GetEx("k", GetExOptions{}); ok {
		t.Fatalf("GetEx of expired key returned ok")
	}
}

func TestSubStr(t *testing.T) {
	s := New()
	s.Set("k", "Hello World", SetOptions{})
	tests := []struct {
		start, stop int
		want        string
	}{
		{0, 4, "Hello"},
		{-5, -1, "World"},
		{0, -1, "Hello World"},
		{6, 100, "World"},
		{5, 3, ""},
	}
	for _, tc := range tests {
		got, err := s.SubStr("k", tc.start, tc.stop)
		if err != nil || got != tc.want {
			t.Fatalf("SubStr(%d,%d) = %q, %v; want %q", tc.start, tc.stop, got, err, tc.want)
		}
	}
}

func TestLcs(t *testing.T) {
	s := New()
	s.Set("a", "ohmytext", SetOptions{})
	s.Set("b", "mynewtext", SetOptions{})

	seq, err := s.Lcs("a", "b")
	if err != nil || seq != "mytext" {
		t.Fatalf("Lcs = %q, %v; want mytext", seq, err)
	}
	n, err := s.LcsLen("a", "b")
	if err != nil || n != 6 {
		t.Fatalf("LcsLen = %d, %v; want 6", n, err)
	}
}

func TestLcsIdx(t *testing.T) {
	s := New()
	s.Set("a", "ohmytext", SetOptions{})
	s.Set("b", "mynewtext", SetOptions{})

	matches, total, err := s.LcsIdx("a", "b")
	if err != nil {
		t.Fatalf("LcsIdx: %v", err)
	}
	if total != 6 {
		t.Fatalf("total = %d, want 6", total)
	}
	want := []LcsMatch{
		{AStart: 4, AEnd: 7, BStart: 5, BEnd: 8, Len: 4},
		{AStart: 2, AEnd: 3, BStart: 0, BEnd: 1, Len: 2},
	}
	if !reflect.DeepEqual(matches, want) {
		t.Fatalf("matches = %+v; want %+v", matches, want)
	}
}

func TestLcsEmpty(t *testing.T) {
	s := New()
	seq, err := s.Lcs("none1", "none2")
	if err != nil || seq != "" {
		t.Fatalf("Lcs empty = %q, %v", seq, err)
	}
}

func BenchmarkLcs(b *testing.B) {
	s := New()
	s.Set("a", "the quick brown fox jumps over the lazy dog", SetOptions{})
	s.Set("b", "a quick brown cat jumped over some lazy dogs", SetOptions{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Lcs("a", "b")
	}
}
