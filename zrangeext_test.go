package redis

import (
	"math"
	"reflect"
	"testing"
)

func TestZRevRangeByScore(t *testing.T) {
	s := New()
	s.ZAdd("z", ZMember{"a", 1}, ZMember{"b", 2}, ZMember{"c", 3}, ZMember{"d", 4})
	full := ScoreRange{Min: math.Inf(-1), Max: math.Inf(1)}
	got, err := s.ZRevRangeByScore("z", full)
	if err != nil {
		t.Fatalf("ZRevRangeByScore: %v", err)
	}
	want := zmembers("d", 4, "c", 3, "b", 2, "a", 1)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ZRevRangeByScore = %v", got)
	}
}

func TestZRangeByScoreLimit(t *testing.T) {
	s := New()
	s.ZAdd("z", ZMember{"a", 1}, ZMember{"b", 2}, ZMember{"c", 3}, ZMember{"d", 4}, ZMember{"e", 5})
	r := ScoreRange{Min: math.Inf(-1), Max: math.Inf(1)}

	got, _ := s.ZRangeByScoreLimit("z", r, 1, 2)
	if !reflect.DeepEqual(got, zmembers("b", 2, "c", 3)) {
		t.Fatalf("limit(1,2) = %v", got)
	}
	got, _ = s.ZRangeByScoreLimit("z", r, 3, -1)
	if !reflect.DeepEqual(got, zmembers("d", 4, "e", 5)) {
		t.Fatalf("limit(3,-1) = %v", got)
	}
	got, _ = s.ZRevRangeByScoreLimit("z", r, 0, 2)
	if !reflect.DeepEqual(got, zmembers("e", 5, "d", 4)) {
		t.Fatalf("rev limit(0,2) = %v", got)
	}
}

func TestZRevRangeByLexAndLimit(t *testing.T) {
	s := New()
	for _, m := range []string{"a", "b", "c", "d"} {
		s.ZAdd("z", ZMember{Member: m, Score: 0})
	}
	full := LexRange{MinInf: true, MaxInf: true}
	got, err := s.ZRevRangeByLex("z", full)
	if err != nil {
		t.Fatalf("ZRevRangeByLex: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"d", "c", "b", "a"}) {
		t.Fatalf("ZRevRangeByLex = %v", got)
	}
	win, _ := s.ZRangeByLexLimit("z", full, 1, 2)
	if !reflect.DeepEqual(win, []string{"b", "c"}) {
		t.Fatalf("ZRangeByLexLimit = %v", win)
	}
}

func TestZRandMemberWithScores(t *testing.T) {
	s := New()
	s.ZAdd("z", ZMember{"a", 1}, ZMember{"b", 2}, ZMember{"c", 3})
	scores := map[string]float64{"a": 1, "b": 2, "c": 3}

	got, err := s.ZRandMemberWithScores("z", 2)
	if err != nil || len(got) != 2 {
		t.Fatalf("ZRandMemberWithScores = %v, %v", got, err)
	}
	for _, m := range got {
		if scores[m.Member] != m.Score {
			t.Fatalf("bad score for %q: %v", m.Member, m.Score)
		}
	}
	if got, _ := s.ZRandMemberWithScores("z", -4); len(got) != 4 {
		t.Fatalf("negative count = %v", got)
	}
}
