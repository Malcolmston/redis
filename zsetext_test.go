package redis

import (
	"reflect"
	"testing"
)

func zmembers(pairs ...any) []ZMember {
	out := make([]ZMember, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, ZMember{Member: pairs[i].(string), Score: float64(pairs[i+1].(int))})
	}
	return out
}

func TestZLexCountAndRemRangeByLex(t *testing.T) {
	s := New()
	for _, m := range []string{"a", "b", "c", "d", "e"} {
		s.ZAdd("z", ZMember{Member: m, Score: 0})
	}
	full := LexRange{MinInf: true, MaxInf: true}
	if n, err := s.ZLexCount("z", full); err != nil || n != 5 {
		t.Fatalf("ZLexCount full = %d, %v; want 5", n, err)
	}
	r := LexRange{Min: "b", Max: "d"}
	if n, _ := s.ZLexCount("z", r); n != 3 {
		t.Fatalf("ZLexCount [b,d] = %d; want 3", n)
	}
	if n, err := s.ZRemRangeByLex("z", r); err != nil || n != 3 {
		t.Fatalf("ZRemRangeByLex = %d, %v; want 3", n, err)
	}
	rest, _ := s.ZRange("z", 0, -1)
	if !reflect.DeepEqual(rest, zmembers("a", 0, "e", 0)) {
		t.Fatalf("remaining = %v", rest)
	}
}

func TestZDiff(t *testing.T) {
	s := New()
	s.ZAdd("a", ZMember{"one", 1}, ZMember{"two", 2}, ZMember{"three", 3})
	s.ZAdd("b", ZMember{"one", 1})
	s.ZAdd("c", ZMember{"two", 2})

	got, err := s.ZDiff("a", "b", "c")
	if err != nil {
		t.Fatalf("ZDiff: %v", err)
	}
	if !reflect.DeepEqual(got, zmembers("three", 3)) {
		t.Fatalf("ZDiff = %v", got)
	}
	n, _ := s.ZDiffStore("dst", "a", "b", "c")
	if n != 1 {
		t.Fatalf("ZDiffStore = %d; want 1", n)
	}
	sc, ok, _ := s.ZScore("dst", "three")
	if !ok || sc != 3 {
		t.Fatalf("dst[three] = %v, %v", sc, ok)
	}
}

func TestZUnionInter(t *testing.T) {
	s := New()
	s.ZAdd("a", ZMember{"x", 1}, ZMember{"y", 2})
	s.ZAdd("b", ZMember{"y", 3}, ZMember{"z", 4})

	u, err := s.ZUnion([]string{"a", "b"}, nil, ZStoreSum)
	if err != nil {
		t.Fatalf("ZUnion: %v", err)
	}
	if !reflect.DeepEqual(u, zmembers("x", 1, "z", 4, "y", 5)) {
		t.Fatalf("ZUnion = %v", u)
	}
	i, _ := s.ZInter([]string{"a", "b"}, nil, ZStoreSum)
	if !reflect.DeepEqual(i, zmembers("y", 5)) {
		t.Fatalf("ZInter = %v", i)
	}
	if n, _ := s.ZInterCard(0, "a", "b"); n != 1 {
		t.Fatalf("ZInterCard = %d; want 1", n)
	}
	// Weighted union: a*2, b*1 → x=2, y=2*2+3=7, z=4.
	w, _ := s.ZUnion([]string{"a", "b"}, []float64{2, 1}, ZStoreSum)
	if !reflect.DeepEqual(w, zmembers("x", 2, "z", 4, "y", 7)) {
		t.Fatalf("weighted ZUnion = %v", w)
	}
	// Max aggregation on intersection: y = max(2*1, 3*1) = 3.
	m, _ := s.ZInter([]string{"a", "b"}, nil, ZStoreMax)
	if !reflect.DeepEqual(m, zmembers("y", 3)) {
		t.Fatalf("ZInter max = %v", m)
	}
}

func TestZRangeStore(t *testing.T) {
	s := New()
	s.ZAdd("src", ZMember{"a", 1}, ZMember{"b", 2}, ZMember{"c", 3}, ZMember{"d", 4})
	n, err := s.ZRangeStore("dst", "src", 1, 2, false)
	if err != nil || n != 2 {
		t.Fatalf("ZRangeStore = %d, %v; want 2", n, err)
	}
	got, _ := s.ZRange("dst", 0, -1)
	if !reflect.DeepEqual(got, zmembers("b", 2, "c", 3)) {
		t.Fatalf("dst = %v", got)
	}
	// Empty range deletes dst.
	if n, _ := s.ZRangeStore("dst", "src", 5, 6, false); n != 0 {
		t.Fatalf("empty ZRangeStore = %d", n)
	}
	if s.Exists("dst") != 0 {
		t.Fatalf("dst not deleted on empty result")
	}
}

func TestZMPop(t *testing.T) {
	s := New()
	s.ZAdd("z2", ZMember{"a", 1}, ZMember{"b", 2}, ZMember{"c", 3})
	key, got, ok, err := s.ZMPopMin(2, "z1", "z2")
	if err != nil || !ok || key != "z2" {
		t.Fatalf("ZMPopMin key = %q, ok=%v, err=%v", key, ok, err)
	}
	if !reflect.DeepEqual(got, zmembers("a", 1, "b", 2)) {
		t.Fatalf("ZMPopMin = %v", got)
	}
	key, got, ok, _ = s.ZMPopMax(5, "z1", "z2")
	if !ok || key != "z2" || !reflect.DeepEqual(got, zmembers("c", 3)) {
		t.Fatalf("ZMPopMax = %q, %v, %v", key, got, ok)
	}
	if _, _, ok, _ := s.ZMPopMin(1, "z1", "z2"); ok {
		t.Fatalf("ZMPop should find nothing")
	}
}

func TestZRandMember(t *testing.T) {
	s := New()
	src := map[string]bool{"a": true, "b": true, "c": true}
	s.ZAdd("z", ZMember{"a", 1}, ZMember{"b", 2}, ZMember{"c", 3})

	// Positive count: distinct members, all from the set.
	got, err := s.ZRandMember("z", 2)
	if err != nil || len(got) != 2 {
		t.Fatalf("ZRandMember pos = %v, %v", got, err)
	}
	seen := map[string]bool{}
	for _, m := range got {
		if !src[m] || seen[m] {
			t.Fatalf("bad member %q in %v", m, got)
		}
		seen[m] = true
	}
	// Count exceeding size is capped.
	if got, _ := s.ZRandMember("z", 10); len(got) != 3 {
		t.Fatalf("ZRandMember cap = %v", got)
	}
	// Negative count: exactly -count, repeats allowed.
	if got, _ := s.ZRandMember("z", -5); len(got) != 5 {
		t.Fatalf("ZRandMember neg = %v", got)
	}
}
