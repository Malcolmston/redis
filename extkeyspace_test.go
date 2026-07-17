package redis

import (
	"errors"
	"testing"
	"time"
)

func TestSetRange(t *testing.T) {
	cases := []struct {
		name    string
		seed    string // initial value; "" means key absent
		offset  int
		val     string
		wantLen int
		wantStr string
		wantErr error
	}{
		{name: "into empty pads", offset: 5, val: "hi", wantLen: 7, wantStr: "\x00\x00\x00\x00\x00hi"},
		{name: "overwrite middle", seed: "Hello World", offset: 6, val: "Redis", wantLen: 11, wantStr: "Hello Redis"},
		{name: "extend past end", seed: "Hi", offset: 4, val: "X", wantLen: 5, wantStr: "Hi\x00\x00X"},
		{name: "at zero", seed: "abcdef", offset: 0, val: "XY", wantLen: 6, wantStr: "XYcdef"},
		{name: "empty val keeps existing", seed: "abc", offset: 1, val: "", wantLen: 3, wantStr: "abc"},
		{name: "empty val missing key", offset: 3, val: "", wantLen: 0, wantStr: ""},
		{name: "negative offset", seed: "abc", offset: -1, val: "z", wantErr: ErrOutOfRange},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			if tc.seed != "" {
				s.Set("k", tc.seed, SetOptions{})
			}
			got, err := s.SetRange("k", tc.offset, tc.val)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if got != tc.wantLen {
				t.Fatalf("len = %d, want %d", got, tc.wantLen)
			}
			if v, _, _ := s.Get("k"); v != tc.wantStr {
				t.Fatalf("value = %q, want %q", v, tc.wantStr)
			}
		})
	}

	t.Run("wrong type", func(t *testing.T) {
		s := New()
		if _, err := s.LPush("l", "a"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SetRange("l", 0, "x"); !errors.Is(err, ErrWrongType) {
			t.Fatalf("err = %v, want ErrWrongType", err)
		}
	})
}

func TestGetRange(t *testing.T) {
	cases := []struct {
		name  string
		seed  string
		start int
		end   int
		want  string
	}{
		{name: "basic", seed: "Hello World", start: 0, end: 4, want: "Hello"},
		{name: "negative both", seed: "Hello World", start: -5, end: -1, want: "World"},
		{name: "clamp end", seed: "Hello", start: 0, end: 100, want: "Hello"},
		{name: "clamp start negative overflow", seed: "Hello", start: -100, end: 2, want: "Hel"},
		{name: "start after end", seed: "Hello", start: 4, end: 2, want: ""},
		{name: "whole via negatives", seed: "abc", start: 0, end: -1, want: "abc"},
		{name: "missing key", seed: "", start: 0, end: 10, want: ""},
		{name: "single char", seed: "abcdef", start: 2, end: 2, want: "c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			if tc.seed != "" {
				s.Set("k", tc.seed, SetOptions{})
			}
			got, err := s.GetRange("k", tc.start, tc.end)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("GetRange = %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("wrong type", func(t *testing.T) {
		s := New()
		if _, err := s.LPush("l", "a"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.GetRange("l", 0, 1); !errors.Is(err, ErrWrongType) {
			t.Fatalf("err = %v, want ErrWrongType", err)
		}
	})
}

func TestSetExPSetEx(t *testing.T) {
	t.Run("setex sets ttl", func(t *testing.T) {
		s, clk := newTestStore()
		if err := s.SetEx("k", 10, "v"); err != nil {
			t.Fatalf("SetEx err: %v", err)
		}
		if v, ok, _ := s.Get("k"); !ok || v != "v" {
			t.Fatalf("Get = %q,%v", v, ok)
		}
		clk.Advance(9 * time.Second)
		if _, ok, _ := s.Get("k"); !ok {
			t.Fatal("key should still be live at 9s")
		}
		clk.Advance(2 * time.Second)
		if _, ok, _ := s.Get("k"); ok {
			t.Fatal("key should have expired past 10s")
		}
	})

	t.Run("psetex sets ttl", func(t *testing.T) {
		s, clk := newTestStore()
		if err := s.PSetEx("k", 500, "v"); err != nil {
			t.Fatalf("PSetEx err: %v", err)
		}
		clk.Advance(400 * time.Millisecond)
		if _, ok, _ := s.Get("k"); !ok {
			t.Fatal("key should be live at 400ms")
		}
		clk.Advance(200 * time.Millisecond)
		if _, ok, _ := s.Get("k"); ok {
			t.Fatal("key should have expired past 500ms")
		}
	})

	t.Run("non-positive seconds", func(t *testing.T) {
		s := New()
		for _, sec := range []int{0, -1} {
			if err := s.SetEx("k", sec, "v"); !errors.Is(err, ErrSyntax) {
				t.Fatalf("SetEx(%d) err = %v, want ErrSyntax", sec, err)
			}
		}
		if err := s.PSetEx("k", 0, "v"); !errors.Is(err, ErrSyntax) {
			t.Fatalf("PSetEx(0) err = %v, want ErrSyntax", err)
		}
	})
}

func TestSetNX(t *testing.T) {
	s := New()
	ok, err := s.SetNX("k", "first")
	if err != nil || !ok {
		t.Fatalf("first SetNX = %v,%v", ok, err)
	}
	ok, err = s.SetNX("k", "second")
	if err != nil || ok {
		t.Fatalf("second SetNX = %v,%v, want false", ok, err)
	}
	if v, _, _ := s.Get("k"); v != "first" {
		t.Fatalf("value = %q, want first", v)
	}
}

func TestMSetAndMGet(t *testing.T) {
	t.Run("mset then mget", func(t *testing.T) {
		s := New()
		if err := s.MSet("a", "1", "b", "2", "c", "3"); err != nil {
			t.Fatalf("MSet err: %v", err)
		}
		got := s.MGet("a", "b", "missing", "c")
		want := []string{"1", "2", "", "3"}
		if len(got) != len(want) {
			t.Fatalf("len = %d, want %d", len(got), len(want))
		}
		for i, w := range want {
			if i == 2 {
				if got[i] != nil {
					t.Fatalf("element %d = %v, want nil", i, *got[i])
				}
				continue
			}
			if got[i] == nil || *got[i] != w {
				t.Fatalf("element %d = %v, want %q", i, got[i], w)
			}
		}
	})

	t.Run("mget non-string is nil", func(t *testing.T) {
		s := New()
		if _, err := s.LPush("l", "x"); err != nil {
			t.Fatal(err)
		}
		got := s.MGet("l")
		if len(got) != 1 || got[0] != nil {
			t.Fatalf("MGet = %v, want [nil]", got)
		}
	})

	t.Run("odd args", func(t *testing.T) {
		s := New()
		if err := s.MSet("a", "1", "b"); !errors.Is(err, ErrWrongArgs) {
			t.Fatalf("err = %v, want ErrWrongArgs", err)
		}
		if err := s.MSet(); !errors.Is(err, ErrWrongArgs) {
			t.Fatalf("empty MSet err = %v, want ErrWrongArgs", err)
		}
	})
}

func TestMSetNX(t *testing.T) {
	t.Run("all new", func(t *testing.T) {
		s := New()
		ok, err := s.MSetNX("a", "1", "b", "2")
		if err != nil || !ok {
			t.Fatalf("MSetNX = %v,%v", ok, err)
		}
		if v, _, _ := s.Get("a"); v != "1" {
			t.Fatalf("a = %q", v)
		}
	})

	t.Run("all or nothing", func(t *testing.T) {
		s := New()
		s.Set("b", "existing", SetOptions{})
		ok, err := s.MSetNX("a", "1", "b", "2")
		if err != nil || ok {
			t.Fatalf("MSetNX = %v,%v, want false", ok, err)
		}
		if _, exists, _ := s.Get("a"); exists {
			t.Fatal("a should not have been created")
		}
		if v, _, _ := s.Get("b"); v != "existing" {
			t.Fatalf("b = %q, want existing", v)
		}
	})

	t.Run("odd args", func(t *testing.T) {
		s := New()
		if _, err := s.MSetNX("a"); !errors.Is(err, ErrWrongArgs) {
			t.Fatalf("err = %v, want ErrWrongArgs", err)
		}
	})
}

func TestIncrByFloat(t *testing.T) {
	cases := []struct {
		name    string
		seed    string
		delta   float64
		want    float64
		wantStr string
		wantErr error
	}{
		{name: "from missing", delta: 3.5, want: 3.5, wantStr: "3.5"},
		{name: "add to existing", seed: "10.5", delta: 0.1, want: 10.6, wantStr: "10.6"},
		{name: "integer value", seed: "5", delta: 2, want: 7, wantStr: "7"},
		{name: "negative delta", seed: "3", delta: -5, want: -2, wantStr: "-2"},
		{name: "bad value", seed: "abc", delta: 1, wantErr: ErrNotFloat},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			if tc.seed != "" {
				s.Set("k", tc.seed, SetOptions{})
			}
			got, err := s.IncrByFloat("k", tc.delta)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				return
			}
			if got != tc.want {
				t.Fatalf("value = %v, want %v", got, tc.want)
			}
			if v, _, _ := s.Get("k"); v != tc.wantStr {
				t.Fatalf("stored = %q, want %q", v, tc.wantStr)
			}
		})
	}

	t.Run("wrong type", func(t *testing.T) {
		s := New()
		if _, err := s.LPush("l", "x"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.IncrByFloat("l", 1); !errors.Is(err, ErrWrongType) {
			t.Fatalf("err = %v, want ErrWrongType", err)
		}
	})
}

func TestRename(t *testing.T) {
	t.Run("moves value and ttl", func(t *testing.T) {
		s, clk := newTestStore()
		if err := s.SetEx("src", 10, "v"); err != nil {
			t.Fatal(err)
		}
		if err := s.Rename("src", "dst"); err != nil {
			t.Fatalf("Rename err: %v", err)
		}
		if _, ok, _ := s.Get("src"); ok {
			t.Fatal("src should be gone")
		}
		if v, ok, _ := s.Get("dst"); !ok || v != "v" {
			t.Fatalf("dst = %q,%v", v, ok)
		}
		clk.Advance(11 * time.Second)
		if _, ok, _ := s.Get("dst"); ok {
			t.Fatal("dst TTL should have carried over and expired")
		}
	})

	t.Run("missing src", func(t *testing.T) {
		s := New()
		if err := s.Rename("nope", "dst"); !errors.Is(err, ErrNoSuchKey) {
			t.Fatalf("err = %v, want ErrNoSuchKey", err)
		}
	})

	t.Run("same key no-op", func(t *testing.T) {
		s := New()
		s.Set("k", "v", SetOptions{})
		if err := s.Rename("k", "k"); err != nil {
			t.Fatalf("err: %v", err)
		}
		if v, ok, _ := s.Get("k"); !ok || v != "v" {
			t.Fatalf("k = %q,%v", v, ok)
		}
	})

	t.Run("overwrites dst", func(t *testing.T) {
		s := New()
		s.Set("src", "new", SetOptions{})
		s.Set("dst", "old", SetOptions{})
		if err := s.Rename("src", "dst"); err != nil {
			t.Fatal(err)
		}
		if v, _, _ := s.Get("dst"); v != "new" {
			t.Fatalf("dst = %q, want new", v)
		}
	})
}

func TestRenameNX(t *testing.T) {
	t.Run("dst absent", func(t *testing.T) {
		s := New()
		s.Set("src", "v", SetOptions{})
		ok, err := s.RenameNX("src", "dst")
		if err != nil || !ok {
			t.Fatalf("RenameNX = %v,%v", ok, err)
		}
	})

	t.Run("dst exists", func(t *testing.T) {
		s := New()
		s.Set("src", "v", SetOptions{})
		s.Set("dst", "keep", SetOptions{})
		ok, err := s.RenameNX("src", "dst")
		if err != nil || ok {
			t.Fatalf("RenameNX = %v,%v, want false", ok, err)
		}
		if v, _, _ := s.Get("dst"); v != "keep" {
			t.Fatalf("dst = %q, want keep", v)
		}
		if _, ok, _ := s.Get("src"); !ok {
			t.Fatal("src should remain")
		}
	})

	t.Run("missing src", func(t *testing.T) {
		s := New()
		if _, err := s.RenameNX("nope", "dst"); !errors.Is(err, ErrNoSuchKey) {
			t.Fatalf("err = %v, want ErrNoSuchKey", err)
		}
	})
}

func TestCopy(t *testing.T) {
	t.Run("deep copy independent", func(t *testing.T) {
		s := New()
		if _, err := s.RPush("src", "a", "b"); err != nil {
			t.Fatal(err)
		}
		ok, err := s.Copy("src", "dst", false)
		if err != nil || !ok {
			t.Fatalf("Copy = %v,%v", ok, err)
		}
		// Mutating src must not affect dst.
		if _, err := s.RPush("src", "c"); err != nil {
			t.Fatal(err)
		}
		got, err := s.LRange("dst", 0, -1)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"a", "b"}
		if len(got) != len(want) {
			t.Fatalf("dst = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("dst[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("dst exists without replace", func(t *testing.T) {
		s := New()
		s.Set("src", "v", SetOptions{})
		s.Set("dst", "keep", SetOptions{})
		ok, err := s.Copy("src", "dst", false)
		if err != nil || ok {
			t.Fatalf("Copy = %v,%v, want false", ok, err)
		}
		if v, _, _ := s.Get("dst"); v != "keep" {
			t.Fatalf("dst = %q, want keep", v)
		}
	})

	t.Run("dst exists with replace", func(t *testing.T) {
		s := New()
		s.Set("src", "v", SetOptions{})
		s.Set("dst", "keep", SetOptions{})
		ok, err := s.Copy("src", "dst", true)
		if err != nil || !ok {
			t.Fatalf("Copy = %v,%v", ok, err)
		}
		if v, _, _ := s.Get("dst"); v != "v" {
			t.Fatalf("dst = %q, want v", v)
		}
	})

	t.Run("missing src", func(t *testing.T) {
		s := New()
		ok, err := s.Copy("nope", "dst", true)
		if err != nil || ok {
			t.Fatalf("Copy = %v,%v, want false", ok, err)
		}
	})

	t.Run("same key", func(t *testing.T) {
		s := New()
		s.Set("k", "v", SetOptions{})
		ok, err := s.Copy("k", "k", true)
		if err != nil || ok {
			t.Fatalf("Copy = %v,%v, want false", ok, err)
		}
	})
}

func TestCopyItemDeepCopy(t *testing.T) {
	s := New()
	s.HSet("h", "f", "1")
	s.SAdd("st", "m1", "m2")
	s.ZAdd("z", ZMember{Member: "a", Score: 1}, ZMember{Member: "b", Score: 2})
	s.RPush("l", "x")
	s.Set("str", "hello", SetOptions{})

	for _, key := range []string{"h", "st", "z", "l", "str"} {
		orig := s.data[key]
		cp := copyItem(orig)
		if cp == orig {
			t.Fatalf("%s: copy returned same pointer", key)
		}
		if cp.kind != orig.kind {
			t.Fatalf("%s: kind = %v, want %v", key, cp.kind, orig.kind)
		}
		if cp.zset != nil && cp.zset == orig.zset {
			t.Fatalf("%s: zset shared", key)
		}
	}

	if copyItem(nil) != nil {
		t.Fatal("copyItem(nil) should be nil")
	}
}

func TestRandomKey(t *testing.T) {
	t.Run("empty store", func(t *testing.T) {
		s := New()
		if k, ok := s.RandomKey(); ok {
			t.Fatalf("RandomKey = %q,%v, want false", k, ok)
		}
	})

	t.Run("returns a live member", func(t *testing.T) {
		s := New()
		keys := map[string]bool{"a": true, "b": true, "c": true}
		for k := range keys {
			s.Set(k, "v", SetOptions{})
		}
		for i := 0; i < 50; i++ {
			k, ok := s.RandomKey()
			if !ok || !keys[k] {
				t.Fatalf("RandomKey = %q,%v", k, ok)
			}
		}
	})

	t.Run("single key", func(t *testing.T) {
		s := New()
		s.Set("only", "v", SetOptions{})
		k, ok := s.RandomKey()
		if !ok || k != "only" {
			t.Fatalf("RandomKey = %q,%v", k, ok)
		}
	})
}

func TestObject(t *testing.T) {
	s := New()
	s.Set("intkey", "12345", SetOptions{})
	s.Set("strkey", "hello world", SetOptions{})
	s.RPush("listkey", "a")
	s.HSet("hashkey", "f", "v")
	s.SAdd("setkey", "m")
	s.ZAdd("zsetkey", ZMember{Member: "m", Score: 1})

	cases := []struct {
		key      string
		encoding string
	}{
		{"intkey", "int"},
		{"strkey", "embstr"},
		{"listkey", "listpack"},
		{"hashkey", "hashtable"},
		{"setkey", "hashtable"},
		{"zsetkey", "skiplist"},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			info, ok := s.Object(tc.key)
			if !ok {
				t.Fatalf("Object(%q) not found", tc.key)
			}
			if info.Encoding != tc.encoding {
				t.Fatalf("encoding = %q, want %q", info.Encoding, tc.encoding)
			}
			if info.RefCount != 1 {
				t.Fatalf("refcount = %d, want 1", info.RefCount)
			}
			if info.IdleTime != 0 {
				t.Fatalf("idletime = %d, want 0", info.IdleTime)
			}
		})
	}

	t.Run("long string is raw", func(t *testing.T) {
		s := New()
		long := ""
		for i := 0; i < 50; i++ {
			long += "x"
		}
		s.Set("long", long, SetOptions{})
		info, ok := s.Object("long")
		if !ok || info.Encoding != "raw" {
			t.Fatalf("encoding = %q, want raw", info.Encoding)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		s := New()
		if _, ok := s.Object("nope"); ok {
			t.Fatal("Object should report missing key")
		}
	})
}

func TestTouchAndUnlink(t *testing.T) {
	t.Run("touch counts existing", func(t *testing.T) {
		s := New()
		s.Set("a", "1", SetOptions{})
		s.Set("b", "2", SetOptions{})
		// "a" appears twice and is counted twice; "missing" is absent.
		if n := s.Touch("a", "b", "missing", "a"); n != 3 {
			t.Fatalf("Touch = %d, want 3", n)
		}
		// Touch does not remove keys.
		if s.Exists("a") != 1 {
			t.Fatal("Touch must not remove keys")
		}
	})

	t.Run("unlink removes like del", func(t *testing.T) {
		s := New()
		s.Set("a", "1", SetOptions{})
		s.Set("b", "2", SetOptions{})
		if n := s.Unlink("a", "b", "missing"); n != 2 {
			t.Fatalf("Unlink = %d, want 2", n)
		}
		if s.Exists("a", "b") != 0 {
			t.Fatal("keys should be removed")
		}
	})

	t.Run("touch skips expired", func(t *testing.T) {
		s, clk := newTestStore()
		s.SetEx("t", 5, "v")
		clk.Advance(6 * time.Second)
		if n := s.Touch("t"); n != 0 {
			t.Fatalf("Touch = %d, want 0", n)
		}
	})
}
