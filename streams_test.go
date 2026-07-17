package redis

import (
	"reflect"
	"testing"
	"time"
)

// newStreamStore returns a Store backed by a ManualClock fixed at a known time
// so auto-generated stream IDs and idle times are deterministic. The clock's
// UnixMilli value is streamTestMs.
func newStreamStore() (*Store, *ManualClock) {
	clk := NewManualClock(time.Unix(1_700_000_000, 0))
	return NewWithClock(clk), clk
}

// streamTestMs is the millisecond value of the fixed test clock.
const streamTestMs = uint64(1_700_000_000_000)

func TestStreamIDStringAndCompare(t *testing.T) {
	tests := []struct {
		id   StreamID
		want string
	}{
		{StreamID{0, 0}, "0-0"},
		{StreamID{5, 0}, "5-0"},
		{StreamID{5, 9}, "5-9"},
	}
	for _, tc := range tests {
		if got := tc.id.String(); got != tc.want {
			t.Errorf("String(%v) = %q, want %q", tc.id, got, tc.want)
		}
	}

	cmp := []struct {
		a, b StreamID
		want int
	}{
		{StreamID{1, 0}, StreamID{2, 0}, -1},
		{StreamID{2, 0}, StreamID{1, 0}, 1},
		{StreamID{1, 1}, StreamID{1, 2}, -1},
		{StreamID{1, 2}, StreamID{1, 1}, 1},
		{StreamID{3, 3}, StreamID{3, 3}, 0},
	}
	for _, tc := range cmp {
		if got := tc.a.Compare(tc.b); got != tc.want {
			t.Errorf("Compare(%v,%v) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestParseStreamID(t *testing.T) {
	tests := []struct {
		in      string
		want    StreamID
		wantErr bool
	}{
		{"5", StreamID{5, 0}, false},
		{"5-2", StreamID{5, 2}, false},
		{"5-*", StreamID{5, streamSeqAuto}, false},
		{"", StreamID{}, true},
		{"x", StreamID{}, true},
		{"5-y", StreamID{}, true},
	}
	for _, tc := range tests {
		got, err := ParseStreamID(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseStreamID(%q) expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseStreamID(%q) unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseStreamID(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestXAddAutoAndExplicit(t *testing.T) {
	s, _ := newStreamStore()

	// Auto IDs increment the sequence within the same millisecond.
	id1, err := s.XAdd("st", "*", "a", "1")
	if err != nil {
		t.Fatalf("XAdd auto: %v", err)
	}
	if id1 != (StreamID{streamTestMs, 0}) {
		t.Fatalf("id1 = %v, want %v", id1, StreamID{streamTestMs, 0})
	}
	id2, _ := s.XAdd("st", "*", "b", "2")
	if id2 != (StreamID{streamTestMs, 1}) {
		t.Fatalf("id2 = %v, want %v", id2, StreamID{streamTestMs, 1})
	}

	// Odd field count is rejected.
	if _, err := s.XAdd("st", "*", "lonely"); err != ErrWrongArgs {
		t.Fatalf("odd fields err = %v, want ErrWrongArgs", err)
	}

	// Explicit ID must exceed the last one.
	if _, err := s.XAdd("st", "1-1", "c", "3"); err == nil {
		t.Fatalf("expected error for smaller explicit ID")
	}

	// A larger explicit ID is accepted.
	if got, err := s.XAdd("st", "9999999999999-5", "d", "4"); err != nil || got.String() != "9999999999999-5" {
		t.Fatalf("explicit add = %v,%v", got, err)
	}

	if n := s.XLen("st"); n != 3 {
		t.Fatalf("XLen = %d, want 3", n)
	}
	if n := s.XLen("missing"); n != 0 {
		t.Fatalf("XLen(missing) = %d, want 0", n)
	}
}

func TestXAddMsAutoSeq(t *testing.T) {
	s, _ := newStreamStore()
	a, _ := s.XAdd("st", "100-*", "f", "v")
	b, _ := s.XAdd("st", "100-*", "f", "v")
	if a != (StreamID{100, 0}) || b != (StreamID{100, 1}) {
		t.Fatalf("ms-* seq = %v,%v want 100-0,100-1", a, b)
	}
}

func TestStreamNamespaceIndependentOfData(t *testing.T) {
	s, _ := newStreamStore()
	s.Set("dup", "plainvalue", SetOptions{})
	if _, err := s.XAdd("dup", "*", "f", "v"); err != nil {
		t.Fatalf("XAdd on shared name: %v", err)
	}
	// The string value is untouched by the stream living under the same name.
	if v, ok, _ := s.Get("dup"); !ok || v != "plainvalue" {
		t.Fatalf("Get(dup) = %q,%v; stream namespace leaked into data", v, ok)
	}
	if s.TypeOf("dup") != TypeString {
		t.Fatalf("TypeOf(dup) = %v, want string", s.TypeOf("dup"))
	}
	if s.XLen("dup") != 1 {
		t.Fatalf("stream length lost")
	}
}

func TestXRangeRevRangeAndCount(t *testing.T) {
	s, _ := newStreamStore()
	s.XAdd("st", "1-0", "a", "1")
	s.XAdd("st", "2-0", "b", "2")
	s.XAdd("st", "2-1", "c", "3")
	s.XAdd("st", "3-0", "d", "4")

	ids := func(entries []StreamEntry) []string {
		out := make([]string, len(entries))
		for i, e := range entries {
			out[i] = e.ID.String()
		}
		return out
	}

	full, _ := s.XRange("st", "-", "+", 0)
	if got, want := ids(full), []string{"1-0", "2-0", "2-1", "3-0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("XRange full = %v, want %v", got, want)
	}

	// Incomplete "2" upper bound covers both 2-0 and 2-1.
	mid, _ := s.XRange("st", "2", "2", 0)
	if got, want := ids(mid), []string{"2-0", "2-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("XRange 2..2 = %v, want %v", got, want)
	}

	capped, _ := s.XRange("st", "-", "+", 2)
	if got, want := ids(capped), []string{"1-0", "2-0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("XRange count=2 = %v, want %v", got, want)
	}

	rev, _ := s.XRevRange("st", "+", "-", 0)
	if got, want := ids(rev), []string{"3-0", "2-1", "2-0", "1-0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("XRevRange = %v, want %v", got, want)
	}

	revCap, _ := s.XRevRange("st", "+", "-", 1)
	if got, want := ids(revCap), []string{"3-0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("XRevRange count=1 = %v, want %v", got, want)
	}
}

func TestXDel(t *testing.T) {
	s, _ := newStreamStore()
	s.XAdd("st", "1-0", "a", "1")
	s.XAdd("st", "2-0", "b", "2")
	n, err := s.XDel("st", "1-0", "9-9")
	if err != nil || n != 1 {
		t.Fatalf("XDel = %d,%v want 1,nil", n, err)
	}
	if s.XLen("st") != 1 {
		t.Fatalf("len after del = %d, want 1", s.XLen("st"))
	}
	// last ID is not reset: re-adding a small ID must still fail.
	if _, err := s.XAdd("st", "1-5", "c", "3"); err == nil {
		t.Fatalf("expected monotonic error after delete")
	}
}

func TestXRead(t *testing.T) {
	s, _ := newStreamStore()
	s.XAdd("st", "1-0", "a", "1")
	s.XAdd("st", "2-0", "b", "2")
	s.XAdd("st", "3-0", "c", "3")

	res, err := s.XRead(0, map[string]string{"st": "1-0"})
	if err != nil {
		t.Fatalf("XRead: %v", err)
	}
	got := res["st"]
	if len(got) != 2 || got[0].ID.String() != "2-0" || got[1].ID.String() != "3-0" {
		t.Fatalf("XRead after 1-0 = %v", got)
	}

	// "$" means only entries newer than the current tail: nothing.
	res2, _ := s.XRead(0, map[string]string{"st": "$"})
	if _, ok := res2["st"]; ok {
		t.Fatalf("XRead $ should yield no key, got %v", res2)
	}

	// count caps the result.
	res3, _ := s.XRead(1, map[string]string{"st": "0"})
	if len(res3["st"]) != 1 || res3["st"][0].ID.String() != "1-0" {
		t.Fatalf("XRead count=1 = %v", res3["st"])
	}
}

func TestConsumerGroupLifecycle(t *testing.T) {
	s, clk := newStreamStore()
	s.XAdd("st", "1-0", "a", "1")
	s.XAdd("st", "2-0", "b", "2")

	// Creating a group on a missing stream without MKSTREAM fails.
	if err := s.XGroupCreate("missing", "g", "0", false); err == nil {
		t.Fatalf("expected error creating group on missing stream")
	}
	if err := s.XGroupCreate("st", "g", "0", false); err != nil {
		t.Fatalf("XGroupCreate: %v", err)
	}
	// Duplicate group creation fails.
	if err := s.XGroupCreate("st", "g", "0", false); err == nil {
		t.Fatalf("expected BUSYGROUP error")
	}

	// Consumer c1 reads new entries with ">".
	res, err := s.XReadGroup("g", "c1", 0, map[string]string{"st": ">"})
	if err != nil {
		t.Fatalf("XReadGroup: %v", err)
	}
	if len(res["st"]) != 2 {
		t.Fatalf("group read got %d entries, want 2", len(res["st"]))
	}

	// Second ">" read yields nothing new.
	res2, _ := s.XReadGroup("g", "c1", 0, map[string]string{"st": ">"})
	if _, ok := res2["st"]; ok {
		t.Fatalf("second > read should be empty, got %v", res2["st"])
	}

	// XPending reports both entries owned by c1.
	cnt, min, max, per, err := s.XPending("st", "g")
	if err != nil {
		t.Fatalf("XPending: %v", err)
	}
	if cnt != 2 || min.String() != "1-0" || max.String() != "2-0" || per["c1"] != 2 {
		t.Fatalf("XPending = cnt=%d min=%v max=%v per=%v", cnt, min, max, per)
	}

	// Idle time reflects the clock advancing.
	clk.Advance(500 * time.Millisecond)
	reread, _ := s.XReadGroup("g", "c1", 0, map[string]string{"st": "0"})
	if len(reread["st"]) != 2 {
		t.Fatalf("re-read pending got %d, want 2", len(reread["st"]))
	}

	// Ack one entry.
	acked, err := s.XAck("st", "g", "1-0")
	if err != nil || acked != 1 {
		t.Fatalf("XAck = %d,%v want 1,nil", acked, err)
	}
	cnt, _, _, per, _ = s.XPending("st", "g")
	if cnt != 1 || per["c1"] != 1 {
		t.Fatalf("after ack cnt=%d per=%v", cnt, per)
	}

	// Destroy the group.
	ok, err := s.XGroupDestroy("st", "g")
	if err != nil || !ok {
		t.Fatalf("XGroupDestroy = %v,%v", ok, err)
	}
	if _, _, _, _, err := s.XPending("st", "g"); err == nil {
		t.Fatalf("XPending on destroyed group should error")
	}
	if ok, _ := s.XGroupDestroy("st", "g"); ok {
		t.Fatalf("second destroy should report false")
	}
}

func TestXReadGroupNoGroup(t *testing.T) {
	s, _ := newStreamStore()
	s.XAdd("st", "1-0", "a", "1")
	if _, err := s.XReadGroup("nope", "c", 0, map[string]string{"st": ">"}); err == nil {
		t.Fatalf("expected NOGROUP error")
	}
}

func TestDispatchStreamCommands(t *testing.T) {
	s, _ := newStreamStore()

	id, err := s.Do("XADD", "st", "1-0", "a", "1")
	if err != nil || id.(string) != "1-0" {
		t.Fatalf("Do XADD = %v,%v", id, err)
	}
	s.Do("XADD", "st", "2-0", "b", "2")

	n, err := s.Do("XLEN", "st")
	if err != nil || n.(int64) != 2 {
		t.Fatalf("Do XLEN = %v,%v", n, err)
	}

	r, err := s.Do("XRANGE", "st", "-", "+")
	if err != nil {
		t.Fatalf("Do XRANGE: %v", err)
	}
	arr := r.([]any)
	if len(arr) != 2 {
		t.Fatalf("XRANGE reply len = %d, want 2", len(arr))
	}
	first := arr[0].([]any)
	if first[0].(string) != "1-0" {
		t.Fatalf("XRANGE first id = %v", first[0])
	}

	rd, err := s.Do("XREAD", "COUNT", "10", "STREAMS", "st", "0")
	if err != nil {
		t.Fatalf("Do XREAD: %v", err)
	}
	if len(rd.([]any)) != 1 {
		t.Fatalf("XREAD reply = %v", rd)
	}

	if _, err := s.Do("XRANGE", "st", "-"); err != ErrSyntax {
		t.Fatalf("XRANGE bad arity err = %v, want ErrSyntax", err)
	}
}
