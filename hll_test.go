package redis

import (
	"errors"
	"fmt"
	"math"
	"testing"
)

func TestHLLRank(t *testing.T) {
	tests := []struct {
		name string
		w    uint64
		want uint8
	}{
		{"top bit set", 1 << (hllRemaining - 1), 1},
		{"second bit set", 1 << (hllRemaining - 2), 2},
		{"lowest bit set", 1, hllRemaining},
		{"no bit set", 0, hllRemaining + 1},
		{"top wins over low", (1 << (hllRemaining - 1)) | 1, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hllRank(tc.w); got != tc.want {
				t.Fatalf("hllRank(%#x) = %d, want %d", tc.w, got, tc.want)
			}
		})
	}
}

func TestHLLHashDeterministic(t *testing.T) {
	for _, in := range []string{"", "a", "hello", "hyperloglog"} {
		if hllHash64(in) != hllHash64(in) {
			t.Fatalf("hllHash64(%q) not deterministic", in)
		}
		idx, rank := hllHashParts(in)
		if idx < 0 || idx >= hllRegisters {
			t.Fatalf("index %d out of range for %q", idx, in)
		}
		if rank < 1 || rank > hllRemaining+1 {
			t.Fatalf("rank %d out of range for %q", rank, in)
		}
	}
}

func TestHLLNewIsValid(t *testing.T) {
	b := hllNewBytes()
	if len(b) != hllDenseLen {
		t.Fatalf("dense length = %d, want %d", len(b), hllDenseLen)
	}
	if !hllValid(string(b)) {
		t.Fatal("freshly created HLL is not valid")
	}
	if hllValid("HYLL") || hllValid("not an hll") || hllValid("") {
		t.Fatal("non-HLL strings reported valid")
	}
}

func TestPFAddCreateAndReadd(t *testing.T) {
	s := New()

	n, err := s.PFAdd("hll", "a", "b", "c")
	if err != nil {
		t.Fatalf("PFAdd: %v", err)
	}
	if n != 1 {
		t.Fatalf("first PFAdd returned %d, want 1 (created)", n)
	}
	if s.TypeOf("hll") != TypeString {
		t.Fatalf("HLL stored as %s, want %s", s.TypeOf("hll"), TypeString)
	}

	// Re-adding the exact same elements changes no register.
	n, err = s.PFAdd("hll", "a", "b", "c")
	if err != nil {
		t.Fatalf("PFAdd re-add: %v", err)
	}
	if n != 0 {
		t.Fatalf("re-adding existing elements returned %d, want 0", n)
	}

	// Adding elements with no arguments to an existing key changes nothing.
	if n, _ = s.PFAdd("hll"); n != 0 {
		t.Fatalf("PFAdd with no elements returned %d, want 0", n)
	}
}

func TestPFAddEmptyCreate(t *testing.T) {
	s := New()
	n, err := s.PFAdd("empty")
	if err != nil {
		t.Fatalf("PFAdd: %v", err)
	}
	if n != 1 {
		t.Fatalf("creating empty HLL returned %d, want 1", n)
	}
	cnt, err := s.PFCount("empty")
	if err != nil {
		t.Fatalf("PFCount: %v", err)
	}
	if cnt != 0 {
		t.Fatalf("empty HLL count = %d, want 0", cnt)
	}
}

func TestPFCountAccuracy(t *testing.T) {
	tests := []struct {
		name string
		n    int
		tol  float64 // allowed relative error
	}{
		{"tiny", 3, 0.5},
		{"small", 100, 0.10},
		{"medium", 1000, 0.05},
		{"large", 10000, 0.05},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			for i := 0; i < tc.n; i++ {
				if _, err := s.PFAdd("k", fmt.Sprintf("elem-%d", i)); err != nil {
					t.Fatalf("PFAdd: %v", err)
				}
			}
			got, err := s.PFCount("k")
			if err != nil {
				t.Fatalf("PFCount: %v", err)
			}
			relErr := math.Abs(float64(got)-float64(tc.n)) / float64(tc.n)
			if relErr > tc.tol {
				t.Fatalf("PFCount = %d for %d distinct (rel err %.4f > tol %.4f)",
					got, tc.n, relErr, tc.tol)
			}
		})
	}
}

func TestPFCountReaddStable(t *testing.T) {
	s := New()
	for i := 0; i < 500; i++ {
		s.PFAdd("k", fmt.Sprintf("v-%d", i))
	}
	// Re-adding every element must never change a register.
	for i := 0; i < 500; i++ {
		if n, _ := s.PFAdd("k", fmt.Sprintf("v-%d", i)); n != 0 {
			t.Fatalf("re-adding v-%d changed a register", i)
		}
	}
}

func TestPFCountMissing(t *testing.T) {
	s := New()
	cnt, err := s.PFCount("nope")
	if err != nil {
		t.Fatalf("PFCount missing: %v", err)
	}
	if cnt != 0 {
		t.Fatalf("missing key count = %d, want 0", cnt)
	}
	if _, err := s.PFCount(); !errors.Is(err, ErrWrongArgs) {
		t.Fatalf("PFCount() error = %v, want ErrWrongArgs", err)
	}
}

func TestPFCountUnionNoPersist(t *testing.T) {
	s := New()
	for i := 0; i < 1000; i++ {
		s.PFAdd("a", fmt.Sprintf("x-%d", i))
	}
	for i := 500; i < 1500; i++ {
		s.PFAdd("b", fmt.Sprintf("x-%d", i))
	}
	// Union of a and b covers x-0 .. x-1499 => ~1500 distinct.
	got, err := s.PFCount("a", "b")
	if err != nil {
		t.Fatalf("PFCount union: %v", err)
	}
	relErr := math.Abs(float64(got)-1500) / 1500
	if relErr > 0.05 {
		t.Fatalf("union count = %d, want ~1500 (rel err %.4f)", got, relErr)
	}
	// The multi-key form must not have created or altered any key.
	if s.Exists("a", "b") != 2 || s.DBSize() != 2 {
		t.Fatalf("union PFCount persisted state: dbsize=%d", s.DBSize())
	}
}

func TestPFMerge(t *testing.T) {
	s := New()
	for i := 0; i < 1000; i++ {
		s.PFAdd("a", fmt.Sprintf("x-%d", i))
	}
	for i := 500; i < 1500; i++ {
		s.PFAdd("b", fmt.Sprintf("x-%d", i))
	}
	if err := s.PFMerge("dest", "a", "b"); err != nil {
		t.Fatalf("PFMerge: %v", err)
	}
	if s.TypeOf("dest") != TypeString {
		t.Fatalf("dest type = %s, want %s", s.TypeOf("dest"), TypeString)
	}
	got, err := s.PFCount("dest")
	if err != nil {
		t.Fatalf("PFCount dest: %v", err)
	}
	relErr := math.Abs(float64(got)-1500) / 1500
	if relErr > 0.05 {
		t.Fatalf("merged count = %d, want ~1500 (rel err %.4f)", got, relErr)
	}
	// Merging into an existing dest is idempotent for the same sources.
	if err := s.PFMerge("dest", "a", "b"); err != nil {
		t.Fatalf("PFMerge idempotent: %v", err)
	}
	got2, _ := s.PFCount("dest")
	if got2 != got {
		t.Fatalf("re-merge changed estimate: %d -> %d", got, got2)
	}
}

func TestPFMergeAbsentSources(t *testing.T) {
	s := New()
	s.PFAdd("a", "one", "two", "three")
	// Absent sources are treated as empty HLLs.
	if err := s.PFMerge("dest", "a", "ghost"); err != nil {
		t.Fatalf("PFMerge: %v", err)
	}
	got, _ := s.PFCount("dest")
	if got != 3 {
		t.Fatalf("merged count = %d, want 3", got)
	}
}

func TestHLLWrongType(t *testing.T) {
	s := New()
	s.Set("str", "hello", SetOptions{})
	s.LPush("list", "x")

	if _, err := s.PFAdd("str", "x"); !errors.Is(err, ErrWrongType) {
		t.Fatalf("PFAdd on plain string: err = %v, want ErrWrongType", err)
	}
	if _, err := s.PFAdd("list", "x"); !errors.Is(err, ErrWrongType) {
		t.Fatalf("PFAdd on list: err = %v, want ErrWrongType", err)
	}
	if _, err := s.PFCount("str"); !errors.Is(err, ErrWrongType) {
		t.Fatalf("PFCount on plain string: err = %v, want ErrWrongType", err)
	}
	if _, err := s.PFCount("hll_missing", "str"); !errors.Is(err, ErrWrongType) {
		t.Fatalf("PFCount union with plain string: err = %v, want ErrWrongType", err)
	}
	if err := s.PFMerge("dest", "str"); !errors.Is(err, ErrWrongType) {
		t.Fatalf("PFMerge with plain string source: err = %v, want ErrWrongType", err)
	}
	if err := s.PFMerge("str", "hll_missing"); !errors.Is(err, ErrWrongType) {
		t.Fatalf("PFMerge into plain string dest: err = %v, want ErrWrongType", err)
	}
}

func TestHLLDispatch(t *testing.T) {
	s := New()

	r, err := s.Do("PFADD", "k", "a", "b", "c")
	if err != nil {
		t.Fatalf("Do PFADD: %v", err)
	}
	if r != int64(1) {
		t.Fatalf("PFADD reply = %v, want int64(1)", r)
	}

	r, err = s.Do("PFCOUNT", "k")
	if err != nil {
		t.Fatalf("Do PFCOUNT: %v", err)
	}
	if n, ok := r.(int64); !ok || n < 1 {
		t.Fatalf("PFCOUNT reply = %v, want positive int64", r)
	}

	if r, err = s.Do("PFMERGE", "dest", "k"); err != nil {
		t.Fatalf("Do PFMERGE: %v", err)
	}
	if r != SimpleString("OK") {
		t.Fatalf("PFMERGE reply = %v, want OK", r)
	}
	if s.TypeOf("dest") != TypeString {
		t.Fatalf("merged dest type = %s, want %s", s.TypeOf("dest"), TypeString)
	}
}
