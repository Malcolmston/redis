package redis

import (
	"reflect"
	"testing"
	"time"
)

func TestExpireAtAndTime(t *testing.T) {
	clk := NewManualClock(time.Unix(1000, 0))
	s := NewWithClock(clk)
	s.Set("k", "v", SetOptions{})

	future := time.Unix(1100, 0)
	if !s.ExpireAt("k", future) {
		t.Fatalf("ExpireAt returned false")
	}
	tm, code := s.ExpireTime("k")
	if code != TTLValue || !tm.Equal(future) {
		t.Fatalf("ExpireTime = %v, %v", tm, code)
	}
	if _, code := s.PExpireTime("missing"); code != TTLNoKey {
		t.Fatalf("PExpireTime missing = %v", code)
	}
	s.Set("p", "v", SetOptions{})
	if _, code := s.ExpireTime("p"); code != TTLNoExpiry {
		t.Fatalf("ExpireTime no-ttl = %v", code)
	}

	// Past time deletes the key.
	if !s.ExpireAt("p", time.Unix(1, 0)) {
		t.Fatalf("ExpireAt past returned false")
	}
	if s.Exists("p") != 0 {
		t.Fatalf("past ExpireAt did not delete")
	}
}

func TestExpireWith(t *testing.T) {
	clk := NewManualClock(time.Unix(0, 0))
	s := NewWithClock(clk)
	s.Set("k", "v", SetOptions{})

	// NX succeeds when there is no TTL.
	if !s.ExpireWith("k", 100*time.Second, ExpireCondNX) {
		t.Fatalf("NX on no-ttl failed")
	}
	// NX now fails because a TTL exists.
	if s.ExpireWith("k", 50*time.Second, ExpireCondNX) {
		t.Fatalf("NX on ttl succeeded")
	}
	// GT fails when shortening.
	if s.ExpireWith("k", 50*time.Second, ExpireCondGT) {
		t.Fatalf("GT shorten succeeded")
	}
	// GT succeeds when extending.
	if !s.ExpireWith("k", 200*time.Second, ExpireCondGT) {
		t.Fatalf("GT extend failed")
	}
	// LT succeeds when shortening.
	if !s.ExpireWith("k", 10*time.Second, ExpireCondLT) {
		t.Fatalf("LT shorten failed")
	}
	// XX fails on a key without a TTL.
	s.Set("n", "v", SetOptions{})
	if s.ExpireWith("n", 10*time.Second, ExpireCondXX) {
		t.Fatalf("XX on no-ttl succeeded")
	}
}

func TestSort(t *testing.T) {
	s := New()
	s.RPush("l", "3", "1", "2", "10")

	got, err := s.Sort("l", SortOptions{})
	if err != nil || !reflect.DeepEqual(got, []string{"1", "2", "3", "10"}) {
		t.Fatalf("numeric Sort = %v, %v", got, err)
	}
	got, _ = s.Sort("l", SortOptions{Desc: true})
	if !reflect.DeepEqual(got, []string{"10", "3", "2", "1"}) {
		t.Fatalf("desc Sort = %v", got)
	}
	got, _ = s.Sort("l", SortOptions{Alpha: true})
	if !reflect.DeepEqual(got, []string{"1", "10", "2", "3"}) {
		t.Fatalf("alpha Sort = %v", got)
	}
	got, _ = s.Sort("l", SortOptions{Limit: true, Offset: 1, Count: 2})
	if !reflect.DeepEqual(got, []string{"2", "3"}) {
		t.Fatalf("limit Sort = %v", got)
	}

	// Sorting a set works and is numeric.
	s.SAdd("st", "20", "5", "8")
	got, _ = s.Sort("st", SortOptions{})
	if !reflect.DeepEqual(got, []string{"5", "8", "20"}) {
		t.Fatalf("set Sort = %v", got)
	}

	// Non-numeric element without Alpha errors.
	s.RPush("bad", "x", "1")
	if _, err := s.Sort("bad", SortOptions{}); err != ErrNotFloat {
		t.Fatalf("non-numeric Sort = %v, want ErrNotFloat", err)
	}
}

func TestSortStore(t *testing.T) {
	s := New()
	s.RPush("l", "3", "1", "2")
	n, err := s.SortStore("dst", "l", SortOptions{})
	if err != nil || n != 3 {
		t.Fatalf("SortStore = %d, %v", n, err)
	}
	got, _ := s.LRange("dst", 0, -1)
	if !reflect.DeepEqual(got, []string{"1", "2", "3"}) {
		t.Fatalf("stored = %v", got)
	}
}
