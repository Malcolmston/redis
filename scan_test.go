package redis

import (
	"reflect"
	"sort"
	"testing"
	"time"
)

// drainScan repeatedly calls Scan until the cursor returns to 0, accumulating
// every key emitted. It guards against a runaway loop.
func drainScan(t *testing.T, s *Store, match string, count int) []string {
	t.Helper()
	var out []string
	var cursor uint64
	for i := 0; i < 10000; i++ {
		res := s.Scan(cursor, match, count)
		out = append(out, res.Keys...)
		cursor = res.Cursor
		if cursor == 0 {
			return out
		}
	}
	t.Fatal("Scan did not terminate")
	return nil
}

func TestScanFullIteration(t *testing.T) {
	for _, count := range []int{-1, 0, 1, 2, 3, 100} {
		s, _ := newTestStore()
		want := []string{"a", "b", "c", "d", "e"}
		for _, k := range want {
			s.Set(k, "v", SetOptions{})
		}
		got := drainScan(t, s, "", count)
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("count=%d: Scan collected %v, want %v", count, got, want)
		}
	}
}

func TestScanEmpty(t *testing.T) {
	s, _ := newTestStore()
	res := s.Scan(0, "", 10)
	if res.Cursor != 0 {
		t.Fatalf("empty store cursor = %d, want 0", res.Cursor)
	}
	if len(res.Keys) != 0 {
		t.Fatalf("empty store keys = %v, want none", res.Keys)
	}
}

func TestScanMatch(t *testing.T) {
	s, _ := newTestStore()
	for _, k := range []string{"user:1", "user:2", "post:1", "user:3"} {
		s.Set(k, "v", SetOptions{})
	}
	got := drainScan(t, s, "user:*", 1)
	sort.Strings(got)
	want := []string{"user:1", "user:2", "user:3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Scan match = %v, want %v", got, want)
	}
}

func TestScanSkipsExpired(t *testing.T) {
	s, clk := newTestStore()
	s.Set("live", "v", SetOptions{})
	s.Set("gone", "v", SetOptions{})
	s.Expire("gone", time.Second)
	clk.Advance(2 * time.Second)
	got := drainScan(t, s, "", 10)
	if !reflect.DeepEqual(got, []string{"live"}) {
		t.Fatalf("Scan with expiry = %v, want [live]", got)
	}
}

func TestScanOutOfRangeCursor(t *testing.T) {
	s, _ := newTestStore()
	s.Set("a", "v", SetOptions{})
	res := s.Scan(9999, "", 10)
	if res.Cursor != 0 || len(res.Keys) != 0 {
		t.Fatalf("out-of-range cursor = %+v, want empty done", res)
	}
}

func TestHScan(t *testing.T) {
	s, _ := newTestStore()
	s.HSet("h", "f1", "v1", "f2", "v2", "f3", "v3")

	var got []string
	var cursor uint64
	for i := 0; i < 100; i++ {
		res, err := s.HScan("h", cursor, "", 1)
		if err != nil {
			t.Fatalf("HScan error: %v", err)
		}
		got = append(got, res.Pairs...)
		cursor = res.Cursor
		if cursor == 0 {
			break
		}
	}
	want := []string{"f1", "v1", "f2", "v2", "f3", "v3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("HScan pairs = %v, want %v", got, want)
	}
}

func TestHScanMatch(t *testing.T) {
	s, _ := newTestStore()
	s.HSet("h", "af", "1", "bf", "2", "ag", "3")
	res, err := s.HScan("h", 0, "a*", 100)
	if err != nil {
		t.Fatalf("HScan error: %v", err)
	}
	want := []string{"af", "1", "ag", "3"}
	if !reflect.DeepEqual(res.Pairs, want) {
		t.Fatalf("HScan match = %v, want %v", res.Pairs, want)
	}
	if res.Cursor != 0 {
		t.Fatalf("HScan cursor = %d, want 0", res.Cursor)
	}
}

func TestHScanMissingAndWrongType(t *testing.T) {
	s, _ := newTestStore()
	res, err := s.HScan("nope", 0, "", 10)
	if err != nil || res.Cursor != 0 || len(res.Pairs) != 0 {
		t.Fatalf("HScan missing = %+v, %v", res, err)
	}
	s.Set("str", "v", SetOptions{})
	if _, err := s.HScan("str", 0, "", 10); err != ErrWrongType {
		t.Fatalf("HScan wrong type err = %v, want ErrWrongType", err)
	}
}

func TestSScan(t *testing.T) {
	s, _ := newTestStore()
	s.SAdd("set", "a", "b", "c", "d")

	var got []string
	var cursor uint64
	for i := 0; i < 100; i++ {
		res, err := s.SScan("set", cursor, "", 2)
		if err != nil {
			t.Fatalf("SScan error: %v", err)
		}
		got = append(got, res.Keys...)
		cursor = res.Cursor
		if cursor == 0 {
			break
		}
	}
	want := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SScan = %v, want %v", got, want)
	}
}

func TestSScanMissingAndWrongType(t *testing.T) {
	s, _ := newTestStore()
	res, err := s.SScan("nope", 0, "", 10)
	if err != nil || res.Cursor != 0 || len(res.Keys) != 0 {
		t.Fatalf("SScan missing = %+v, %v", res, err)
	}
	s.Set("str", "v", SetOptions{})
	if _, err := s.SScan("str", 0, "", 10); err != ErrWrongType {
		t.Fatalf("SScan wrong type err = %v, want ErrWrongType", err)
	}
}

func TestZScan(t *testing.T) {
	s, _ := newTestStore()
	s.ZAdd("z",
		ZMember{Member: "a", Score: 1},
		ZMember{Member: "b", Score: 2.5},
		ZMember{Member: "c", Score: 3},
	)

	var got []string
	var cursor uint64
	for i := 0; i < 100; i++ {
		res, err := s.ZScan("z", cursor, "", 1)
		if err != nil {
			t.Fatalf("ZScan error: %v", err)
		}
		got = append(got, res.Pairs...)
		cursor = res.Cursor
		if cursor == 0 {
			break
		}
	}
	want := []string{"a", "1", "b", "2.5", "c", "3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ZScan = %v, want %v", got, want)
	}
}

func TestZScanMatchAndErrors(t *testing.T) {
	s, _ := newTestStore()
	s.ZAdd("z",
		ZMember{Member: "apple", Score: 1},
		ZMember{Member: "avocado", Score: 2},
		ZMember{Member: "banana", Score: 3},
	)
	res, err := s.ZScan("z", 0, "a*", 100)
	if err != nil {
		t.Fatalf("ZScan error: %v", err)
	}
	want := []string{"apple", "1", "avocado", "2"}
	if !reflect.DeepEqual(res.Pairs, want) {
		t.Fatalf("ZScan match = %v, want %v", res.Pairs, want)
	}

	if r, err := s.ZScan("missing", 0, "", 10); err != nil || r.Cursor != 0 || len(r.Pairs) != 0 {
		t.Fatalf("ZScan missing = %+v, %v", r, err)
	}
	s.Set("str", "v", SetOptions{})
	if _, err := s.ZScan("str", 0, "", 10); err != ErrWrongType {
		t.Fatalf("ZScan wrong type err = %v, want ErrWrongType", err)
	}
}

func TestScanCursorProgression(t *testing.T) {
	s, _ := newTestStore()
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		s.Set(k, "v", SetOptions{})
	}
	// First page of 2 returns the two lowest keys and a non-zero cursor.
	res := s.Scan(0, "", 2)
	if !reflect.DeepEqual(res.Keys, []string{"a", "b"}) {
		t.Fatalf("page 1 keys = %v, want [a b]", res.Keys)
	}
	if res.Cursor != 2 {
		t.Fatalf("page 1 cursor = %d, want 2", res.Cursor)
	}
	// Final page returns remaining key and cursor 0.
	res = s.Scan(4, "", 2)
	if !reflect.DeepEqual(res.Keys, []string{"e"}) {
		t.Fatalf("final page keys = %v, want [e]", res.Keys)
	}
	if res.Cursor != 0 {
		t.Fatalf("final page cursor = %d, want 0", res.Cursor)
	}
}
