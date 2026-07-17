package redis

import (
	"errors"
	"reflect"
	"sort"
	"testing"
)

// mustZAdd is a test helper that populates a sorted set or fails the test.
func mustZAdd(t *testing.T, s *Store, key string, members ...ZMember) {
	t.Helper()
	if _, err := s.ZAdd(key, members...); err != nil {
		t.Fatalf("ZAdd(%q): %v", key, err)
	}
}

func TestLInsert(t *testing.T) {
	tests := []struct {
		name     string
		before   bool
		pivot    string
		val      string
		wantN    int
		wantList []string
	}{
		{"before existing", true, "b", "x", 4, []string{"a", "x", "b", "c"}},
		{"after existing", false, "b", "x", 4, []string{"a", "b", "x", "c"}},
		{"missing pivot", true, "z", "x", -1, []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			if _, err := s.RPush("l", "a", "b", "c"); err != nil {
				t.Fatal(err)
			}
			n, err := s.LInsert("l", tt.before, tt.pivot, tt.val)
			if err != nil {
				t.Fatalf("LInsert: %v", err)
			}
			if n != tt.wantN {
				t.Errorf("n = %d, want %d", n, tt.wantN)
			}
			got, _ := s.LRange("l", 0, -1)
			if !reflect.DeepEqual(got, tt.wantList) {
				t.Errorf("list = %v, want %v", got, tt.wantList)
			}
		})
	}

	t.Run("missing key", func(t *testing.T) {
		s := New()
		n, err := s.LInsert("nope", true, "a", "x")
		if err != nil || n != 0 {
			t.Fatalf("got (%d,%v), want (0,nil)", n, err)
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		s := New()
		s.Set("k", "v", SetOptions{})
		if _, err := s.LInsert("k", true, "a", "x"); !errors.Is(err, ErrWrongType) {
			t.Fatalf("err = %v, want ErrWrongType", err)
		}
	})
}

func TestLSet(t *testing.T) {
	s := New()
	if _, err := s.RPush("l", "a", "b", "c"); err != nil {
		t.Fatal(err)
	}
	if err := s.LSet("l", 1, "B"); err != nil {
		t.Fatalf("LSet: %v", err)
	}
	if err := s.LSet("l", -1, "C"); err != nil {
		t.Fatalf("LSet neg: %v", err)
	}
	got, _ := s.LRange("l", 0, -1)
	if want := []string{"a", "B", "C"}; !reflect.DeepEqual(got, want) {
		t.Errorf("list = %v, want %v", got, want)
	}
	if err := s.LSet("l", 5, "x"); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("out of range err = %v, want ErrOutOfRange", err)
	}
	if err := s.LSet("missing", 0, "x"); !errors.Is(err, ErrNoSuchKey) {
		t.Errorf("missing err = %v, want ErrNoSuchKey", err)
	}
}

func TestLRem(t *testing.T) {
	tests := []struct {
		name  string
		count int
		want  int
		list  []string
	}{
		{"head two", 2, 2, []string{"b", "a", "c"}},
		{"tail two", -2, 2, []string{"a", "b", "c"}},
		{"all", 0, 3, []string{"b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			if _, err := s.RPush("l", "a", "b", "a", "a", "c"); err != nil {
				t.Fatal(err)
			}
			n, err := s.LRem("l", tt.count, "a")
			if err != nil {
				t.Fatalf("LRem: %v", err)
			}
			if n != tt.want {
				t.Errorf("n = %d, want %d", n, tt.want)
			}
			got, _ := s.LRange("l", 0, -1)
			if !reflect.DeepEqual(got, tt.list) {
				t.Errorf("list = %v, want %v", got, tt.list)
			}
		})
	}

	t.Run("empties key", func(t *testing.T) {
		s := New()
		if _, err := s.RPush("l", "a", "a"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.LRem("l", 0, "a"); err != nil {
			t.Fatal(err)
		}
		if s.Exists("l") != 0 {
			t.Errorf("key should be deleted after removing all elements")
		}
	})
}

func TestLMoveAndRPopLPush(t *testing.T) {
	t.Run("across lists", func(t *testing.T) {
		s := New()
		if _, err := s.RPush("src", "a", "b", "c"); err != nil {
			t.Fatal(err)
		}
		val, ok, err := s.LMove("src", "dst", ListRight, ListLeft)
		if err != nil || !ok || val != "c" {
			t.Fatalf("LMove = (%q,%v,%v), want (c,true,nil)", val, ok, err)
		}
		gotSrc, _ := s.LRange("src", 0, -1)
		gotDst, _ := s.LRange("dst", 0, -1)
		if !reflect.DeepEqual(gotSrc, []string{"a", "b"}) {
			t.Errorf("src = %v", gotSrc)
		}
		if !reflect.DeepEqual(gotDst, []string{"c"}) {
			t.Errorf("dst = %v", gotDst)
		}
	})

	t.Run("rotate same list", func(t *testing.T) {
		s := New()
		if _, err := s.RPush("l", "a", "b", "c"); err != nil {
			t.Fatal(err)
		}
		val, ok, err := s.RPopLPush("l", "l")
		if err != nil || !ok || val != "c" {
			t.Fatalf("RPopLPush = (%q,%v,%v)", val, ok, err)
		}
		got, _ := s.LRange("l", 0, -1)
		if want := []string{"c", "a", "b"}; !reflect.DeepEqual(got, want) {
			t.Errorf("list = %v, want %v", got, want)
		}
	})

	t.Run("empty source deletes key", func(t *testing.T) {
		s := New()
		if _, err := s.RPush("src", "only"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.LMove("src", "dst", ListLeft, ListRight); err != nil {
			t.Fatal(err)
		}
		if s.Exists("src") != 0 {
			t.Errorf("src should be deleted when drained")
		}
	})

	t.Run("missing source", func(t *testing.T) {
		s := New()
		_, ok, err := s.LMove("nope", "dst", ListLeft, ListRight)
		if ok || err != nil {
			t.Fatalf("got (%v,%v), want (false,nil)", ok, err)
		}
	})

	t.Run("wrong type dst", func(t *testing.T) {
		s := New()
		if _, err := s.RPush("src", "a"); err != nil {
			t.Fatal(err)
		}
		s.Set("dst", "v", SetOptions{})
		if _, _, err := s.LMove("src", "dst", ListLeft, ListRight); !errors.Is(err, ErrWrongType) {
			t.Fatalf("err = %v, want ErrWrongType", err)
		}
	})
}

func TestLTrim(t *testing.T) {
	t.Run("keeps range", func(t *testing.T) {
		s := New()
		if _, err := s.RPush("l", "a", "b", "c", "d", "e"); err != nil {
			t.Fatal(err)
		}
		if err := s.LTrim("l", 1, 3); err != nil {
			t.Fatal(err)
		}
		got, _ := s.LRange("l", 0, -1)
		if want := []string{"b", "c", "d"}; !reflect.DeepEqual(got, want) {
			t.Errorf("list = %v, want %v", got, want)
		}
	})

	t.Run("empty range deletes key", func(t *testing.T) {
		s := New()
		if _, err := s.RPush("l", "a", "b"); err != nil {
			t.Fatal(err)
		}
		if err := s.LTrim("l", 5, 10); err != nil {
			t.Fatal(err)
		}
		if s.Exists("l") != 0 {
			t.Errorf("key should be deleted for empty trim range")
		}
	})
}

func TestLPos(t *testing.T) {
	s := New()
	if _, err := s.RPush("l", "a", "b", "c", "b", "b"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		val   string
		rank  int
		count int
		want  []int
	}{
		{"first", "b", 1, 0, []int{1, 3, 4}},
		{"from second", "b", 2, 0, []int{3, 4}},
		{"count limit", "b", 1, 2, []int{1, 3}},
		{"reverse", "b", -1, 0, []int{4, 3, 1}},
		{"missing", "z", 1, 0, []int{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.LPos("l", tt.val, tt.rank, tt.count)
			if err != nil {
				t.Fatalf("LPos: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHIncrBy(t *testing.T) {
	s := New()
	n, err := s.HIncrBy("h", "f", 5)
	if err != nil || n != 5 {
		t.Fatalf("got (%d,%v), want (5,nil)", n, err)
	}
	n, _ = s.HIncrBy("h", "f", -2)
	if n != 3 {
		t.Errorf("n = %d, want 3", n)
	}
	if _, err := s.HSet("h", "s", "notint"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.HIncrBy("h", "s", 1); !errors.Is(err, ErrNotInteger) {
		t.Errorf("err = %v, want ErrNotInteger", err)
	}
}

func TestHIncrByFloat(t *testing.T) {
	s := New()
	f, err := s.HIncrByFloat("h", "f", 1.5)
	if err != nil || f != 1.5 {
		t.Fatalf("got (%v,%v), want (1.5,nil)", f, err)
	}
	f, _ = s.HIncrByFloat("h", "f", 2.5)
	if f != 4.0 {
		t.Errorf("f = %v, want 4", f)
	}
	if _, err := s.HSet("h", "s", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.HIncrByFloat("h", "s", 1); !errors.Is(err, ErrNotFloat) {
		t.Errorf("err = %v, want ErrNotFloat", err)
	}
}

func TestHMGet(t *testing.T) {
	s := New()
	if _, err := s.HSet("h", "a", "1", "b", "2"); err != nil {
		t.Fatal(err)
	}
	got := s.HMGet("h", "a", "missing", "b")
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0] == nil || *got[0] != "1" {
		t.Errorf("got[0] = %v, want 1", got[0])
	}
	if got[1] != nil {
		t.Errorf("got[1] = %v, want nil", got[1])
	}
	if got[2] == nil || *got[2] != "2" {
		t.Errorf("got[2] = %v, want 2", got[2])
	}

	// Wrong type or missing key yields all-nil.
	s.Set("str", "v", SetOptions{})
	for i, v := range s.HMGet("str", "a", "b") {
		if v != nil {
			t.Errorf("wrong-type HMGet[%d] = %v, want nil", i, v)
		}
	}
}

func TestHSetNX(t *testing.T) {
	s := New()
	ok, err := s.HSetNX("h", "f", "1")
	if err != nil || !ok {
		t.Fatalf("first = (%v,%v), want (true,nil)", ok, err)
	}
	ok, _ = s.HSetNX("h", "f", "2")
	if ok {
		t.Errorf("second = true, want false")
	}
	v, _, _ := s.HGet("h", "f")
	if v != "1" {
		t.Errorf("value = %q, want 1", v)
	}
}

func TestHRandField(t *testing.T) {
	s := New()
	if _, err := s.HSet("h", "a", "1", "b", "2", "c", "3"); err != nil {
		t.Fatal(err)
	}
	all := map[string]string{"a": "1", "b": "2", "c": "3"}

	t.Run("distinct", func(t *testing.T) {
		got, err := s.HRandField("h", 2, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		seen := map[string]bool{}
		for _, f := range got {
			if _, ok := all[f]; !ok {
				t.Errorf("unexpected field %q", f)
			}
			if seen[f] {
				t.Errorf("duplicate field %q with positive count", f)
			}
			seen[f] = true
		}
	})

	t.Run("distinct capped at size", func(t *testing.T) {
		got, _ := s.HRandField("h", 10, false)
		if len(got) != 3 {
			t.Errorf("len = %d, want 3", len(got))
		}
	})

	t.Run("with repeats", func(t *testing.T) {
		got, _ := s.HRandField("h", -5, false)
		if len(got) != 5 {
			t.Errorf("len = %d, want 5", len(got))
		}
		for _, f := range got {
			if _, ok := all[f]; !ok {
				t.Errorf("unexpected field %q", f)
			}
		}
	})

	t.Run("with values", func(t *testing.T) {
		got, _ := s.HRandField("h", 2, true)
		if len(got) != 4 {
			t.Fatalf("len = %d, want 4", len(got))
		}
		for i := 0; i < len(got); i += 2 {
			if all[got[i]] != got[i+1] {
				t.Errorf("value mismatch for %q: got %q", got[i], got[i+1])
			}
		}
	})

	t.Run("missing key", func(t *testing.T) {
		got, err := s.HRandField("nope", 3, false)
		if err != nil || len(got) != 0 {
			t.Fatalf("got (%v,%v)", got, err)
		}
	})
}

func TestHStrlen(t *testing.T) {
	s := New()
	if _, err := s.HSet("h", "f", "hello"); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.HStrlen("h", "f"); n != 5 {
		t.Errorf("n = %d, want 5", n)
	}
	if n, _ := s.HStrlen("h", "missing"); n != 0 {
		t.Errorf("missing field n = %d, want 0", n)
	}
	if n, _ := s.HStrlen("nokey", "f"); n != 0 {
		t.Errorf("missing key n = %d, want 0", n)
	}
}

func TestSPop(t *testing.T) {
	t.Run("subset removed", func(t *testing.T) {
		s := New()
		if _, err := s.SAdd("s", "a", "b", "c", "d"); err != nil {
			t.Fatal(err)
		}
		got, err := s.SPop("s", 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("popped %d, want 2", len(got))
		}
		remaining, _ := s.SCard("s")
		if remaining != 2 {
			t.Errorf("remaining = %d, want 2", remaining)
		}
		for _, m := range got {
			if ok, _ := s.SIsMember("s", m); ok {
				t.Errorf("member %q still present after pop", m)
			}
		}
	})

	t.Run("pop all deletes key", func(t *testing.T) {
		s := New()
		if _, err := s.SAdd("s", "a", "b"); err != nil {
			t.Fatal(err)
		}
		got, _ := s.SPop("s", 5)
		sort.Strings(got)
		if !reflect.DeepEqual(got, []string{"a", "b"}) {
			t.Errorf("got %v, want [a b]", got)
		}
		if s.Exists("s") != 0 {
			t.Errorf("key should be deleted after popping all")
		}
	})

	t.Run("non-positive count", func(t *testing.T) {
		s := New()
		if _, err := s.SAdd("s", "a"); err != nil {
			t.Fatal(err)
		}
		got, _ := s.SPop("s", 0)
		if len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})
}

func TestSRandMember(t *testing.T) {
	s := New()
	if _, err := s.SAdd("s", "a", "b", "c"); err != nil {
		t.Fatal(err)
	}
	members := map[string]bool{"a": true, "b": true, "c": true}

	t.Run("distinct", func(t *testing.T) {
		got, _ := s.SRandMember("s", 2)
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		seen := map[string]bool{}
		for _, m := range got {
			if !members[m] {
				t.Errorf("unexpected member %q", m)
			}
			if seen[m] {
				t.Errorf("duplicate %q", m)
			}
			seen[m] = true
		}
		if c, _ := s.SCard("s"); c != 3 {
			t.Errorf("SRandMember must not remove; card = %d", c)
		}
	})

	t.Run("with repeats", func(t *testing.T) {
		got, _ := s.SRandMember("s", -5)
		if len(got) != 5 {
			t.Errorf("len = %d, want 5", len(got))
		}
		for _, m := range got {
			if !members[m] {
				t.Errorf("unexpected member %q", m)
			}
		}
	})
}

func TestSMove(t *testing.T) {
	t.Run("moves member", func(t *testing.T) {
		s := New()
		if _, err := s.SAdd("src", "a", "b"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SAdd("dst", "c"); err != nil {
			t.Fatal(err)
		}
		ok, err := s.SMove("src", "dst", "a")
		if err != nil || !ok {
			t.Fatalf("got (%v,%v), want (true,nil)", ok, err)
		}
		if in, _ := s.SIsMember("src", "a"); in {
			t.Error("a still in src")
		}
		if in, _ := s.SIsMember("dst", "a"); !in {
			t.Error("a not in dst")
		}
	})

	t.Run("member absent", func(t *testing.T) {
		s := New()
		if _, err := s.SAdd("src", "a"); err != nil {
			t.Fatal(err)
		}
		ok, err := s.SMove("src", "dst", "z")
		if err != nil || ok {
			t.Fatalf("got (%v,%v), want (false,nil)", ok, err)
		}
	})

	t.Run("drains source", func(t *testing.T) {
		s := New()
		if _, err := s.SAdd("src", "only"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SMove("src", "dst", "only"); err != nil {
			t.Fatal(err)
		}
		if s.Exists("src") != 0 {
			t.Error("src should be deleted when drained")
		}
	})
}

func TestSetStoreOps(t *testing.T) {
	setup := func() *Store {
		s := New()
		if _, err := s.SAdd("a", "1", "2", "3"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SAdd("b", "2", "3", "4"); err != nil {
			t.Fatal(err)
		}
		return s
	}

	t.Run("inter", func(t *testing.T) {
		s := setup()
		n, err := s.SInterStore("out", "a", "b")
		if err != nil || n != 2 {
			t.Fatalf("got (%d,%v), want (2,nil)", n, err)
		}
		got, _ := s.SMembers("out")
		if want := []string{"2", "3"}; !reflect.DeepEqual(got, want) {
			t.Errorf("out = %v, want %v", got, want)
		}
	})

	t.Run("union", func(t *testing.T) {
		s := setup()
		n, _ := s.SUnionStore("out", "a", "b")
		if n != 4 {
			t.Errorf("n = %d, want 4", n)
		}
		got, _ := s.SMembers("out")
		if want := []string{"1", "2", "3", "4"}; !reflect.DeepEqual(got, want) {
			t.Errorf("out = %v, want %v", got, want)
		}
	})

	t.Run("diff", func(t *testing.T) {
		s := setup()
		n, _ := s.SDiffStore("out", "a", "b")
		if n != 1 {
			t.Errorf("n = %d, want 1", n)
		}
		got, _ := s.SMembers("out")
		if want := []string{"1"}; !reflect.DeepEqual(got, want) {
			t.Errorf("out = %v, want %v", got, want)
		}
	})

	t.Run("empty result deletes dst", func(t *testing.T) {
		s := setup()
		s.Set("out", "preexisting", SetOptions{})
		n, err := s.SInterStore("out", "a", "nonexistent")
		if err != nil || n != 0 {
			t.Fatalf("got (%d,%v), want (0,nil)", n, err)
		}
		if s.Exists("out") != 0 {
			t.Error("dst should be deleted for empty result")
		}
	})
}

func TestZIncrBy(t *testing.T) {
	s := New()
	f, err := s.ZIncrBy("z", 2.5, "m")
	if err != nil || f != 2.5 {
		t.Fatalf("got (%v,%v), want (2.5,nil)", f, err)
	}
	f, _ = s.ZIncrBy("z", 1.5, "m")
	if f != 4.0 {
		t.Errorf("f = %v, want 4", f)
	}
	sc, ok, _ := s.ZScore("z", "m")
	if !ok || sc != 4.0 {
		t.Errorf("score = %v (%v), want 4", sc, ok)
	}
}

func TestZCount(t *testing.T) {
	s := New()
	mustZAdd(t, s, "z",
		ZMember{Member: "a", Score: 1},
		ZMember{Member: "b", Score: 2},
		ZMember{Member: "c", Score: 3},
	)
	n, err := s.ZCount("z", ScoreRange{Min: 1, Max: 2})
	if err != nil || n != 2 {
		t.Fatalf("got (%d,%v), want (2,nil)", n, err)
	}
	n, _ = s.ZCount("z", ScoreRange{Min: 1, Max: 3, MinExclusive: true})
	if n != 2 {
		t.Errorf("exclusive min n = %d, want 2", n)
	}
}

func TestZPopMinMax(t *testing.T) {
	build := func() *Store {
		s := New()
		mustZAdd(t, s, "z",
			ZMember{Member: "a", Score: 1},
			ZMember{Member: "b", Score: 2},
			ZMember{Member: "c", Score: 3},
		)
		return s
	}

	t.Run("min", func(t *testing.T) {
		s := build()
		got, err := s.ZPopMin("z", 2)
		if err != nil {
			t.Fatal(err)
		}
		want := []ZMember{{Member: "a", Score: 1}, {Member: "b", Score: 2}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
		if c, _ := s.ZCard("z"); c != 1 {
			t.Errorf("card = %d, want 1", c)
		}
	})

	t.Run("max", func(t *testing.T) {
		s := build()
		got, _ := s.ZPopMax("z", 2)
		want := []ZMember{{Member: "c", Score: 3}, {Member: "b", Score: 2}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("pop all deletes key", func(t *testing.T) {
		s := build()
		if _, err := s.ZPopMin("z", 10); err != nil {
			t.Fatal(err)
		}
		if s.Exists("z") != 0 {
			t.Error("key should be deleted after popping all")
		}
	})
}

func TestZMScore(t *testing.T) {
	s := New()
	mustZAdd(t, s, "z",
		ZMember{Member: "a", Score: 1.5},
		ZMember{Member: "b", Score: 2.5},
	)
	got := s.ZMScore("z", "a", "missing", "b")
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0] == nil || *got[0] != 1.5 {
		t.Errorf("got[0] = %v, want 1.5", got[0])
	}
	if got[1] != nil {
		t.Errorf("got[1] = %v, want nil", got[1])
	}
	if got[2] == nil || *got[2] != 2.5 {
		t.Errorf("got[2] = %v, want 2.5", got[2])
	}
}

func TestZRangeByLex(t *testing.T) {
	s := New()
	mustZAdd(t, s, "z",
		ZMember{Member: "a", Score: 0},
		ZMember{Member: "b", Score: 0},
		ZMember{Member: "c", Score: 0},
		ZMember{Member: "d", Score: 0},
	)
	tests := []struct {
		name string
		r    LexRange
		want []string
	}{
		{"full", LexRange{MinInf: true, MaxInf: true}, []string{"a", "b", "c", "d"}},
		{"inclusive", LexRange{Min: "b", Max: "c"}, []string{"b", "c"}},
		{"exclusive", LexRange{Min: "a", Max: "d", MinExclusive: true, MaxExclusive: true}, []string{"b", "c"}},
		{"min to c", LexRange{MinInf: true, Max: "c"}, []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ZRangeByLex("z", tt.r)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestZRemRangeByRank(t *testing.T) {
	s := New()
	mustZAdd(t, s, "z",
		ZMember{Member: "a", Score: 1},
		ZMember{Member: "b", Score: 2},
		ZMember{Member: "c", Score: 3},
		ZMember{Member: "d", Score: 4},
	)
	n, err := s.ZRemRangeByRank("z", 1, 2)
	if err != nil || n != 2 {
		t.Fatalf("got (%d,%v), want (2,nil)", n, err)
	}
	got, _ := s.ZRange("z", 0, -1)
	want := []ZMember{{Member: "a", Score: 1}, {Member: "d", Score: 4}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("remaining = %v, want %v", got, want)
	}
}

func TestZRemRangeByScore(t *testing.T) {
	s := New()
	mustZAdd(t, s, "z",
		ZMember{Member: "a", Score: 1},
		ZMember{Member: "b", Score: 2},
		ZMember{Member: "c", Score: 3},
	)
	n, err := s.ZRemRangeByScore("z", ScoreRange{Min: 2, Max: 3})
	if err != nil || n != 2 {
		t.Fatalf("got (%d,%v), want (2,nil)", n, err)
	}
	got, _ := s.ZRange("z", 0, -1)
	want := []ZMember{{Member: "a", Score: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("remaining = %v, want %v", got, want)
	}

	// Removing all elements deletes the key.
	if _, err := s.ZRemRangeByScore("z", ScoreRange{Min: 0, Max: 10}); err != nil {
		t.Fatal(err)
	}
	if s.Exists("z") != 0 {
		t.Error("key should be deleted after removing all")
	}
}

func TestZUnionStore(t *testing.T) {
	build := func() *Store {
		s := New()
		mustZAdd(t, s, "a", ZMember{Member: "x", Score: 1}, ZMember{Member: "y", Score: 2})
		mustZAdd(t, s, "b", ZMember{Member: "y", Score: 3}, ZMember{Member: "z", Score: 4})
		return s
	}

	t.Run("sum", func(t *testing.T) {
		s := build()
		n, err := s.ZUnionStore("out", []string{"a", "b"}, nil, ZStoreSum)
		if err != nil || n != 3 {
			t.Fatalf("got (%d,%v), want (3,nil)", n, err)
		}
		if sc, _, _ := s.ZScore("out", "y"); sc != 5 {
			t.Errorf("y score = %v, want 5", sc)
		}
		if sc, _, _ := s.ZScore("out", "x"); sc != 1 {
			t.Errorf("x score = %v, want 1", sc)
		}
	})

	t.Run("weights and max", func(t *testing.T) {
		s := build()
		if _, err := s.ZUnionStore("out", []string{"a", "b"}, []float64{2, 1}, ZStoreMax); err != nil {
			t.Fatal(err)
		}
		// y: max(2*2, 3*1) = 4
		if sc, _, _ := s.ZScore("out", "y"); sc != 4 {
			t.Errorf("y score = %v, want 4", sc)
		}
	})

	t.Run("min", func(t *testing.T) {
		s := build()
		if _, err := s.ZUnionStore("out", []string{"a", "b"}, nil, ZStoreMin); err != nil {
			t.Fatal(err)
		}
		if sc, _, _ := s.ZScore("out", "y"); sc != 2 {
			t.Errorf("y score = %v, want 2", sc)
		}
	})
}

func TestZInterStore(t *testing.T) {
	s := New()
	mustZAdd(t, s, "a", ZMember{Member: "x", Score: 1}, ZMember{Member: "y", Score: 2})
	mustZAdd(t, s, "b", ZMember{Member: "y", Score: 3}, ZMember{Member: "z", Score: 4})

	n, err := s.ZInterStore("out", []string{"a", "b"}, nil, ZStoreSum)
	if err != nil || n != 1 {
		t.Fatalf("got (%d,%v), want (1,nil)", n, err)
	}
	if sc, _, _ := s.ZScore("out", "y"); sc != 5 {
		t.Errorf("y score = %v, want 5", sc)
	}

	t.Run("missing key yields empty and deletes dst", func(t *testing.T) {
		s.Set("out", "x", SetOptions{})
		n, err := s.ZInterStore("out", []string{"a", "nope"}, nil, ZStoreSum)
		if err != nil || n != 0 {
			t.Fatalf("got (%d,%v), want (0,nil)", n, err)
		}
		if s.Exists("out") != 0 {
			t.Error("dst should be deleted for empty intersection")
		}
	})
}

func TestExtWrongType(t *testing.T) {
	s := New()
	s.Set("str", "v", SetOptions{})
	if _, err := s.HIncrBy("str", "f", 1); !errors.Is(err, ErrWrongType) {
		t.Errorf("HIncrBy: %v", err)
	}
	if _, err := s.SPop("str", 1); !errors.Is(err, ErrWrongType) {
		t.Errorf("SPop: %v", err)
	}
	if _, err := s.ZCount("str", ScoreRange{}); !errors.Is(err, ErrWrongType) {
		t.Errorf("ZCount: %v", err)
	}
	if _, err := s.LPos("str", "x", 1, 0); !errors.Is(err, ErrWrongType) {
		t.Errorf("LPos: %v", err)
	}
}
