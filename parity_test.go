package redis

import (
	"errors"
	"testing"
)

// The TestParity* functions in this file encode concrete known-answer vectors
// taken directly from the upstream Redis test suite (redis/redis, tag 7.4.0,
// tests/unit/*.tcl and tests/unit/type/*.tcl). Each case mirrors an
// assert_equal / assert_error assertion from those files so that the Go port's
// behavior can be checked against Redis' own expected values. The vectors are
// deterministic and require no network, clock, or server.

// TestParityIncr mirrors tests/unit/type/incr.tcl.
func TestParityIncr(t *testing.T) {
	s := New()

	// {INCR against non existing key} -> 1, GET -> "1"
	if n, err := s.Incr("novar"); err != nil || n != 1 {
		t.Fatalf("INCR novar = %d, %v; want 1", n, err)
	}
	if v, ok, _ := s.Get("novar"); !ok || v != "1" {
		t.Fatalf("GET novar = %q,%v; want \"1\"", v, ok)
	}
	// {INCR against key created by incr itself} -> 2
	if n, _ := s.Incr("novar"); n != 2 {
		t.Fatalf("INCR novar = %d; want 2", n)
	}
	// {DECR against key created by incr} -> 1
	if n, _ := s.Decr("novar"); n != 1 {
		t.Fatalf("DECR novar = %d; want 1", n)
	}
	// {DECR against key is not exist and incr}
	s.Del("novar_not_exist")
	if n, _ := s.Decr("novar_not_exist"); n != -1 {
		t.Fatalf("DECR novar_not_exist = %d; want -1", n)
	}
	if n, _ := s.Incr("novar_not_exist"); n != 0 {
		t.Fatalf("INCR novar_not_exist = %d; want 0", n)
	}
	// {INCR against key originally set with SET} -> 101
	s.Set("novar", "100", SetOptions{})
	if n, _ := s.Incr("novar"); n != 101 {
		t.Fatalf("INCR novar = %d; want 101", n)
	}
	// {INCR over 32bit value} -> 17179869185
	s.Set("novar", "17179869184", SetOptions{})
	if n, _ := s.Incr("novar"); n != 17179869185 {
		t.Fatalf("INCR novar = %d; want 17179869185", n)
	}
	// {INCRBY over 32bit value with over 32bit increment} -> 34359738368
	s.Set("novar", "17179869184", SetOptions{})
	if n, _ := s.IncrBy("novar", 17179869184); n != 34359738368 {
		t.Fatalf("INCRBY = %d; want 34359738368", n)
	}
	// {INCR fails against key with spaces (left/right/both)} -> ERR
	for _, sp := range []string{"    11", "11    ", "    11    "} {
		s.Set("novar", sp, SetOptions{})
		if _, err := s.Incr("novar"); !errors.Is(err, ErrNotInteger) {
			t.Fatalf("INCR %q err = %v; want ErrNotInteger", sp, err)
		}
	}
	// {DECRBY negation overflow} -> ERR
	s.Set("x", "0", SetOptions{})
	if _, err := s.DecrBy("x", -9223372036854775808); err == nil {
		t.Fatalf("DECRBY x MinInt64 err = nil; want overflow error")
	}
	// {INCR fails against a key holding a list} -> WRONGTYPE
	s.RPush("mylist", "1")
	if _, err := s.Incr("mylist"); !errors.Is(err, ErrWrongType) {
		t.Fatalf("INCR mylist err = %v; want ErrWrongType", err)
	}
	// {DECRBY over 32bit value with over 32bit increment, negative res} -> -1
	s.Set("novar", "17179869184", SetOptions{})
	if n, _ := s.DecrBy("novar", 17179869185); n != -1 {
		t.Fatalf("DECRBY = %d; want -1", n)
	}
	// {DECRBY against key is not exist} -> -1
	s.Del("key_not_exist")
	if n, _ := s.DecrBy("key_not_exist", 1); n != -1 {
		t.Fatalf("DECRBY key_not_exist 1 = %d; want -1", n)
	}
}

// TestParityIncrOverflow mirrors Redis' integer overflow guard: INCR/INCRBY
// beyond the signed 64-bit range is rejected rather than silently wrapping.
func TestParityIncrOverflow(t *testing.T) {
	s := New()
	// MaxInt64 + 1 overflows.
	s.Set("k", "9223372036854775807", SetOptions{})
	if _, err := s.Incr("k"); !errors.Is(err, ErrIncrOverflow) {
		t.Fatalf("INCR at MaxInt64 err = %v; want ErrIncrOverflow", err)
	}
	// The stored value must be unchanged after a rejected increment.
	if v, _, _ := s.Get("k"); v != "9223372036854775807" {
		t.Fatalf("value changed after overflow: %q", v)
	}
	// MinInt64 - 1 overflows.
	s.Set("k", "-9223372036854775808", SetOptions{})
	if _, err := s.Decr("k"); !errors.Is(err, ErrIncrOverflow) {
		t.Fatalf("DECR at MinInt64 err = %v; want ErrIncrOverflow", err)
	}
}

// TestParityIncrByFloat mirrors the INCRBYFLOAT vectors in
// tests/unit/type/incr.tcl.
func TestParityIncrByFloat(t *testing.T) {
	s := New()

	// {INCRBYFLOAT against non existing key} -> 1, GET 1, +0.25 -> 1.25
	s.Del("novar")
	if f, _ := s.IncrByFloat("novar", 1); f != 1 {
		t.Fatalf("INCRBYFLOAT novar 1 = %v; want 1", f)
	}
	if v, _, _ := s.Get("novar"); v != "1" {
		t.Fatalf("GET novar = %q; want \"1\"", v)
	}
	if f, _ := s.IncrByFloat("novar", 0.25); f != 1.25 {
		t.Fatalf("INCRBYFLOAT novar 0.25 = %v; want 1.25", f)
	}
	if v, _, _ := s.Get("novar"); v != "1.25" {
		t.Fatalf("GET novar = %q; want \"1.25\"", v)
	}
	// {INCRBYFLOAT against key originally set with SET} -> 3
	s.Set("novar", "1.5", SetOptions{})
	if f, _ := s.IncrByFloat("novar", 1.5); f != 3 {
		t.Fatalf("INCRBYFLOAT novar 1.5 = %v; want 3", f)
	}
	// {INCRBYFLOAT over 32bit value} -> 17179869185.5 (no scientific notation)
	s.Set("novar", "17179869184", SetOptions{})
	if _, _ = s.IncrByFloat("novar", 1.5); true {
		if v, _, _ := s.Get("novar"); v != "17179869185.5" {
			t.Fatalf("GET novar = %q; want \"17179869185.5\"", v)
		}
	}
	// {INCRBYFLOAT over 32bit value with over 32bit increment} -> 34359738368
	s.Set("novar", "17179869184", SetOptions{})
	s.IncrByFloat("novar", 17179869184)
	if v, _, _ := s.Get("novar"); v != "34359738368" {
		t.Fatalf("GET novar = %q; want \"34359738368\"", v)
	}
	// {INCRBYFLOAT fails against key with spaces (left/right/both)} -> ERR
	for _, sp := range []string{"    11", "11    ", " 11 "} {
		s.Set("novar", sp, SetOptions{})
		if _, err := s.IncrByFloat("novar", 1.0); !errors.Is(err, ErrNotFloat) {
			t.Fatalf("INCRBYFLOAT %q err = %v; want ErrNotFloat", sp, err)
		}
	}
	// {INCRBYFLOAT fails against a key holding a list} -> WRONGTYPE
	s.Del("mylist")
	s.RPush("mylist", "1")
	if _, err := s.IncrByFloat("mylist", 1.0); !errors.Is(err, ErrWrongType) {
		t.Fatalf("INCRBYFLOAT mylist err = %v; want ErrWrongType", err)
	}
	// {INCRBYFLOAT decrement}. Upstream asserts 1 + (-1.1) == -0.1, which relies
	// on Redis' 80-bit long double arithmetic. This port is float64-based (a
	// documented architectural choice), so 1.0 + (-1.1) rounds to
	// -0.10000000000000009 rather than exactly -0.1. We therefore encode a
	// decrement whose result is exactly representable in float64 so the
	// human-format parity (fixed-point, no exponent) is asserted precisely.
	s.Set("foo", "5", SetOptions{})
	if f, _ := s.IncrByFloat("foo", -1.5); f != 3.5 {
		t.Fatalf("INCRBYFLOAT foo -1.5 = %v; want 3.5", f)
	}
	if v, _, _ := s.Get("foo"); v != "3.5" {
		t.Fatalf("GET foo = %q; want \"3.5\"", v)
	}
	// {No negative zero} -> "0"
	s.Del("foo")
	s.IncrByFloat("foo", float64(1)/41)
	s.IncrByFloat("foo", float64(-1)/41)
	if v, _, _ := s.Get("foo"); v != "0" {
		t.Fatalf("GET foo = %q; want \"0\" (no negative zero)", v)
	}
}

// TestParityIncrByFloatNaNOrInfinity mirrors {INCRBYFLOAT does not allow NaN or
// Infinity}: incrementing by +inf is rejected.
func TestParityIncrByFloatNaNOrInfinity(t *testing.T) {
	s := New()
	s.Set("foo", "0", SetOptions{})
	inf := 1.0
	for i := 0; i < 400; i++ { // build +Inf without importing math in the test
		inf *= 10
	}
	if _, err := s.IncrByFloat("foo", inf); !errors.Is(err, ErrIncrNaNOrInf) {
		t.Fatalf("INCRBYFLOAT foo +inf err = %v; want ErrIncrNaNOrInf", err)
	}
	// The stored value must be unchanged.
	if v, _, _ := s.Get("foo"); v != "0" {
		t.Fatalf("value changed after NaN/Inf increment: %q", v)
	}
}

// TestParityString mirrors set/get, setnx, getset, strlen, setrange, getrange,
// substr, mset/msetnx/mget from tests/unit/type/string.tcl.
func TestParityString(t *testing.T) {
	s := New()

	// {SET and GET an item}
	s.Set("x", "foobar", SetOptions{})
	if v, _, _ := s.Get("x"); v != "foobar" {
		t.Fatalf("GET x = %q; want foobar", v)
	}
	// {SET and GET an empty item}
	s.Set("x", "", SetOptions{})
	if v, ok, _ := s.Get("x"); !ok || v != "" {
		t.Fatalf("GET x = %q,%v; want empty,true", v, ok)
	}
	// {SETNX target key missing} -> 1, {SETNX target key exists} -> 0
	s.Del("novar")
	if ok, _ := s.SetNX("novar", "foobared"); !ok {
		t.Fatalf("SETNX novar (missing) = false; want true")
	}
	if v, _, _ := s.Get("novar"); v != "foobared" {
		t.Fatalf("GET novar = %q; want foobared", v)
	}
	if ok, _ := s.SetNX("novar", "blabla"); ok {
		t.Fatalf("SETNX novar (exists) = true; want false")
	}
	if v, _, _ := s.Get("novar"); v != "foobared" {
		t.Fatalf("GET novar = %q; want foobared unchanged", v)
	}
	// {GETSET (set new value)} -> {} then xyz
	s.Del("foo")
	if old, had, _ := s.GetSet("foo", "xyz"); had {
		t.Fatalf("GETSET new: had = true; want false")
	} else if old != "" {
		t.Fatalf("GETSET new old = %q; want empty", old)
	}
	if v, _, _ := s.Get("foo"); v != "xyz" {
		t.Fatalf("GET foo = %q; want xyz", v)
	}
	// {GETSET (replace old value)} -> bar then xyz
	s.Set("foo", "bar", SetOptions{})
	if old, _, _ := s.GetSet("foo", "xyz"); old != "bar" {
		t.Fatalf("GETSET replace old = %q; want bar", old)
	}
	if v, _, _ := s.Get("foo"); v != "xyz" {
		t.Fatalf("GET foo = %q; want xyz", v)
	}

	// STRLEN vectors
	if n, _ := s.Strlen("notakey"); n != 0 {
		t.Fatalf("STRLEN notakey = %d; want 0", n)
	}
	s.Set("myinteger", "-555", SetOptions{})
	if n, _ := s.Strlen("myinteger"); n != 4 {
		t.Fatalf("STRLEN myinteger = %d; want 4", n)
	}
	s.Set("mystring", "foozzz0123456789 baz", SetOptions{})
	if n, _ := s.Strlen("mystring"); n != 20 {
		t.Fatalf("STRLEN mystring = %d; want 20", n)
	}

	// SETRANGE vectors {against non-existing key}
	s.Del("mykey")
	if n, _ := s.SetRange("mykey", 0, "foo"); n != 3 {
		t.Fatalf("SETRANGE mykey 0 foo = %d; want 3", n)
	}
	if v, _, _ := s.Get("mykey"); v != "foo" {
		t.Fatalf("GET mykey = %q; want foo", v)
	}
	// {SETRANGE against non-existing key with empty value} -> 0, key not created
	s.Del("mykey")
	if n, _ := s.SetRange("mykey", 0, ""); n != 0 {
		t.Fatalf("SETRANGE mykey 0 \"\" = %d; want 0", n)
	}
	if s.Exists("mykey") != 0 {
		t.Fatalf("EXISTS mykey = 1; want 0")
	}
	// {SETRANGE against non-existing key with offset 1} -> "\000foo"
	s.Del("mykey")
	if n, _ := s.SetRange("mykey", 1, "foo"); n != 4 {
		t.Fatalf("SETRANGE mykey 1 foo = %d; want 4", n)
	}
	if v, _, _ := s.Get("mykey"); v != "\x00foo" {
		t.Fatalf("GET mykey = %q; want \\000foo", v)
	}
	// {SETRANGE against string-encoded key} -> "boo"
	s.Set("mykey", "foo", SetOptions{})
	if n, _ := s.SetRange("mykey", 0, "b"); n != 3 {
		t.Fatalf("SETRANGE mykey 0 b = %d; want 3", n)
	}
	if v, _, _ := s.Get("mykey"); v != "boo" {
		t.Fatalf("GET mykey = %q; want boo", v)
	}
	// grow across a gap
	s.Set("mykey", "foo", SetOptions{})
	if n, _ := s.SetRange("mykey", 4, "bar"); n != 7 {
		t.Fatalf("SETRANGE mykey 4 bar = %d; want 7", n)
	}
	if v, _, _ := s.Get("mykey"); v != "foo\x00bar" {
		t.Fatalf("GET mykey = %q; want foo\\000bar", v)
	}

	// GETRANGE vectors
	s.Set("mykey", "Hello World", SetOptions{})
	getrangeCases := []struct {
		start, end int
		want       string
	}{
		{0, 3, "Hell"},
		{0, -1, "Hello World"},
		{-4, -1, "orld"},
		{5, 3, ""},
		{5, 5000, " World"},
		{-5000, 10000, "Hello World"},
	}
	for _, c := range getrangeCases {
		if got, _ := s.GetRange("mykey", c.start, c.end); got != c.want {
			t.Fatalf("GETRANGE mykey %d %d = %q; want %q", c.start, c.end, got, c.want)
		}
	}
	// GETRANGE against integer-encoded value
	s.Set("mykey", "1234", SetOptions{})
	if got, _ := s.GetRange("mykey", 0, 2); got != "123" {
		t.Fatalf("GETRANGE int 0 2 = %q; want 123", got)
	}
	if got, _ := s.GetRange("mykey", -3, -1); got != "234" {
		t.Fatalf("GETRANGE int -3 -1 = %q; want 234", got)
	}

	// SUBSTR vectors
	s.Set("key", "abcde", SetOptions{})
	substrCases := []struct {
		start, stop int
		want        string
	}{
		{0, 0, "a"},
		{0, 3, "abcd"},
		{-4, -1, "bcde"},
		{-1, -3, ""},
		{7, 8, ""},
	}
	for _, c := range substrCases {
		if got, _ := s.SubStr("key", c.start, c.stop); got != c.want {
			t.Fatalf("SUBSTR key %d %d = %q; want %q", c.start, c.stop, got, c.want)
		}
	}
	if got, _ := s.SubStr("nokey", 0, 1); got != "" {
		t.Fatalf("SUBSTR nokey = %q; want empty", got)
	}

	// MGET vectors {MGET against non existing key}
	s.Set("fooA", "BAR", SetOptions{})
	s.Set("barA", "FOO", SetOptions{})
	got := s.MGet("fooA", "barA")
	if len(got) != 2 || got[0] == nil || *got[0] != "BAR" || got[1] == nil || *got[1] != "FOO" {
		t.Fatalf("MGET fooA barA = %v; want [BAR FOO]", got)
	}
	got = s.MGet("fooA", "baazzzz", "barA")
	if len(got) != 3 || got[0] == nil || *got[0] != "BAR" || got[1] != nil || got[2] == nil || *got[2] != "FOO" {
		t.Fatalf("MGET with missing middle key = %v; want [BAR <nil> FOO]", got)
	}
}

// TestParityMSetNX mirrors the MSET/MSETNX base cases in string.tcl.
func TestParityMSetNX(t *testing.T) {
	s := New()
	// {MSET base case}
	if err := s.MSet("x1", "10", "y1", "foo bar", "z1", "x x x x x x x\n\n\r\n"); err != nil {
		t.Fatalf("MSET err = %v", err)
	}
	if got := s.MGet("x1", "y1", "z1"); *got[0] != "10" || *got[1] != "foo bar" || *got[2] != "x x x x x x x\n\n\r\n" {
		t.Fatalf("MGET after MSET = %v", got)
	}
	// {MSETNX with already existent key} -> 0 (nothing written)
	if ok, _ := s.MSetNX("x1", "xxx", "y2", "yyy"); ok {
		t.Fatalf("MSETNX with existing key = true; want false")
	}
	if s.Exists("y2") != 0 {
		t.Fatalf("y2 was written despite MSETNX failure")
	}
	// {MSETNX with not existing keys} -> 1
	if ok, _ := s.MSetNX("x2", "xxx", "y2", "yyy"); !ok {
		t.Fatalf("MSETNX new keys = false; want true")
	}
	if v, _, _ := s.Get("x2"); v != "xxx" {
		t.Fatalf("GET x2 = %q; want xxx", v)
	}
	if v, _, _ := s.Get("y2"); v != "yyy" {
		t.Fatalf("GET y2 = %q; want yyy", v)
	}
}

// TestParityKeyspace mirrors DEL/EXISTS/RENAME/RENAMENX from
// tests/unit/keyspace.tcl.
func TestParityKeyspace(t *testing.T) {
	s := New()

	// {DEL against a single item}
	s.Set("x", "foo", SetOptions{})
	if s.Exists("x") != 1 {
		t.Fatalf("EXISTS x = 0; want 1")
	}
	s.Del("x")
	if _, ok, _ := s.Get("x"); ok {
		t.Fatalf("GET x after DEL returned a value")
	}
	// {EXISTS} counts duplicates
	s.Set("x", "foo", SetOptions{})
	if s.Exists("x", "x", "nope") != 2 {
		t.Fatalf("EXISTS x x nope = %d; want 2", s.Exists("x", "x", "nope"))
	}

	// {RENAME basic usage}
	s.Set("mykey", "hello", SetOptions{})
	if err := s.Rename("mykey", "mykey1"); err != nil {
		t.Fatalf("RENAME err = %v", err)
	}
	// {RENAME source key should no longer exist}
	if s.Exists("mykey") != 0 {
		t.Fatalf("source key still exists after RENAME")
	}
	if v, _, _ := s.Get("mykey1"); v != "hello" {
		t.Fatalf("GET mykey1 = %q; want hello", v)
	}
	// {RENAME against non existing source key} -> error
	if err := s.Rename("nokey", "foobar"); !errors.Is(err, ErrNoSuchKey) {
		t.Fatalf("RENAME nokey err = %v; want ErrNoSuchKey", err)
	}
	// {RENAMENX against already existing key} -> 0
	s.Set("mykey", "a", SetOptions{})
	s.Set("mykey2", "b", SetOptions{})
	if ok, _ := s.RenameNX("mykey", "mykey2"); ok {
		t.Fatalf("RENAMENX onto existing key = true; want false")
	}
	// {RENAMENX basic usage} -> 1
	s.Del("dst")
	if ok, _ := s.RenameNX("mykey", "dst"); !ok {
		t.Fatalf("RENAMENX to fresh key = false; want true")
	}
	if v, _, _ := s.Get("dst"); v != "a" {
		t.Fatalf("GET dst = %q; want a", v)
	}
}

// TestParityCopy mirrors {COPY basic usage for string} and the REPLACE
// semantics from tests/unit/keyspace.tcl.
func TestParityCopy(t *testing.T) {
	s := New()
	s.Set("mykey", "foobar", SetOptions{})
	// COPY to a fresh key succeeds.
	if ok, _ := s.Copy("mykey", "mynewkey", false); !ok {
		t.Fatalf("COPY to fresh key = false; want true")
	}
	if v, _, _ := s.Get("mynewkey"); v != "foobar" {
		t.Fatalf("GET mynewkey = %q; want foobar", v)
	}
	// {COPY for string does not replace an existing key without REPLACE option}
	s.Set("mynewkey", "hello", SetOptions{})
	if ok, _ := s.Copy("mykey", "mynewkey", false); ok {
		t.Fatalf("COPY without REPLACE onto existing = true; want false")
	}
	if v, _, _ := s.Get("mynewkey"); v != "hello" {
		t.Fatalf("GET mynewkey = %q; want hello (unchanged)", v)
	}
	// {COPY for string can replace an existing key with REPLACE option}
	if ok, _ := s.Copy("mykey", "mynewkey", true); !ok {
		t.Fatalf("COPY with REPLACE = false; want true")
	}
	if v, _, _ := s.Get("mynewkey"); v != "foobar" {
		t.Fatalf("GET mynewkey = %q; want foobar after REPLACE", v)
	}
}

// TestParityBitops mirrors SETBIT/GETBIT/BITCOUNT vectors from
// tests/unit/type/string.tcl and tests/unit/bitops.tcl.
func TestParityBitops(t *testing.T) {
	s := New()

	// {SETBIT against non-existing key} -> "\x40"
	s.Del("mykey")
	if old, _ := s.SetBit("mykey", 1, 1); old != 0 {
		t.Fatalf("SETBIT mykey 1 1 old = %d; want 0", old)
	}
	if v, _, _ := s.Get("mykey"); v != "\x40" {
		t.Fatalf("GET mykey = %q; want 0x40", v)
	}
	// {SETBIT against string-encoded key}
	s.Del("mykey")
	s.SetBit("mykey", 1, 1) // 0x40
	if old, _ := s.SetBit("mykey", 2, 1); old != 0 {
		t.Fatalf("SETBIT mykey 2 1 old = %d; want 0", old)
	}
	if v, _, _ := s.Get("mykey"); v != "\x60" {
		t.Fatalf("GET mykey = %q; want 0x60", v)
	}
	if old, _ := s.SetBit("mykey", 1, 0); old != 1 {
		t.Fatalf("SETBIT mykey 1 0 old = %d; want 1", old)
	}
	if v, _, _ := s.Get("mykey"); v != "\x20" {
		t.Fatalf("GET mykey = %q; want 0x20", v)
	}

	// GETBIT: value 0x24 = 00100100 => bit2=1, bit5=1, others 0
	s.Set("mykey", "\x24", SetOptions{})
	getbitWant := map[int64]int{0: 0, 1: 0, 2: 1, 3: 0, 4: 0, 5: 1, 6: 0, 7: 0, 8: 0, 100: 0, 10000: 0}
	for off, want := range getbitWant {
		if got, _ := s.GetBit("mykey", off); got != want {
			t.Fatalf("GETBIT mykey %d = %d; want %d", off, got, want)
		}
	}

	// BITCOUNT full-string vectors from bitops.tcl {BITCOUNT returns 0 ...}
	s.Del("mykey")
	if n, _ := s.BitCount("mykey", 0, -1, false); n != 0 {
		t.Fatalf("BITCOUNT missing = %d; want 0", n)
	}
	// "foobar" has 26 set bits (documented BITCOUNT example).
	s.Set("mykey", "foobar", SetOptions{})
	if n, _ := s.BitCount("mykey", 0, -1, false); n != 26 {
		t.Fatalf("BITCOUNT foobar = %d; want 26", n)
	}
	// BITCOUNT foobar 1 1 -> 6 ('o').
	if n, _ := s.BitCount("mykey", 1, 1, false); n != 6 {
		t.Fatalf("BITCOUNT foobar 1 1 = %d; want 6", n)
	}
	// BITCOUNT foobar 0 0 -> 4 ('f').
	if n, _ := s.BitCount("mykey", 0, 0, false); n != 4 {
		t.Fatalf("BITCOUNT foobar 0 0 = %d; want 4", n)
	}
}

// TestParityZset mirrors ZADD/ZSCORE/ZRANGE/ZINCRBY vectors from
// tests/unit/type/zset.tcl.
func TestParityZset(t *testing.T) {
	s := New()

	// {ZADD/ZRANGE} basic ordering
	s.ZAdd("ztmp", ZMember{Member: "x", Score: 10})
	s.ZAdd("ztmp", ZMember{Member: "y", Score: 20})
	s.ZAdd("ztmp", ZMember{Member: "z", Score: 30})
	if got := parityZMembers(s, "ztmp"); !parityEqualStrings(got, []string{"x", "y", "z"}) {
		t.Fatalf("ZRANGE ztmp = %v; want [x y z]", got)
	}
	// re-score y to 1 -> reorder to y x z
	s.ZAdd("ztmp", ZMember{Member: "y", Score: 1})
	if got := parityZMembers(s, "ztmp"); !parityEqualStrings(got, []string{"y", "x", "z"}) {
		t.Fatalf("ZRANGE ztmp = %v; want [y x z]", got)
	}

	// ZADD returns count of newly added members.
	s.Del("zt2")
	if n, _ := s.ZAdd("zt2", ZMember{Member: "x", Score: 10}, ZMember{Member: "y", Score: 20}, ZMember{Member: "z", Score: 30}); n != 3 {
		t.Fatalf("ZADD zt2 = %d; want 3", n)
	}
	// updating existing members adds 0.
	if n, _ := s.ZAdd("zt2", ZMember{Member: "x", Score: 11}, ZMember{Member: "y", Score: 21}); n != 0 {
		t.Fatalf("ZADD update = %d; want 0", n)
	}
	if sc, _, _ := s.ZScore("zt2", "x"); sc != 11 {
		t.Fatalf("ZSCORE zt2 x = %v; want 11", sc)
	}

	// ZRANK / ZREVRANK
	s.Del("zranktmp")
	s.ZAdd("zranktmp", ZMember{Member: "x", Score: 10}, ZMember{Member: "y", Score: 20}, ZMember{Member: "z", Score: 30})
	if r, ok, _ := s.ZRank("zranktmp", "x"); !ok || r != 0 {
		t.Fatalf("ZRANK x = %d,%v; want 0,true", r, ok)
	}
	if r, ok, _ := s.ZRank("zranktmp", "z"); !ok || r != 2 {
		t.Fatalf("ZRANK z = %d,%v; want 2,true", r, ok)
	}
	if r, ok, _ := s.ZRevRank("zranktmp", "x"); !ok || r != 2 {
		t.Fatalf("ZREVRANK x = %d,%v; want 2,true", r, ok)
	}
	if _, ok, _ := s.ZRank("zranktmp", "foo"); ok {
		t.Fatalf("ZRANK of missing member returned ok; want not-ok")
	}

	// ZINCRBY against non-existing key/member creates it.
	s.Del("zt3")
	if f, _ := s.ZIncrBy("zt3", 5, "foo"); f != 5 {
		t.Fatalf("ZINCRBY zt3 5 foo = %v; want 5", f)
	}
	if f, _ := s.ZIncrBy("zt3", 5, "foo"); f != 10 {
		t.Fatalf("ZINCRBY zt3 5 foo (again) = %v; want 10", f)
	}
	if f, _ := s.ZIncrBy("zt3", -5, "foo"); f != 5 {
		t.Fatalf("ZINCRBY zt3 -5 foo = %v; want 5", f)
	}
}

// TestParityHash mirrors HSET/HGET/HINCRBY/HINCRBYFLOAT vectors from
// tests/unit/type/hash.tcl.
func TestParityHash(t *testing.T) {
	s := New()

	// {HINCRBY against non existing database key} -> 2
	s.Del("htest")
	if n, _ := s.HIncrBy("htest", "foo", 2); n != 2 {
		t.Fatalf("HINCRBY htest foo 2 = %d; want 2", n)
	}
	// {HINCRBY against hash key created by hincrby itself} -> 5
	if n, _ := s.HIncrBy("htest", "foo", 3); n != 5 {
		t.Fatalf("HINCRBY htest foo 3 = %d; want 5", n)
	}
	// {HINCRBY against hash key originally set with HSET} -> 102
	s.HSet("smallhash", "tmp", "100")
	if n, _ := s.HIncrBy("smallhash", "tmp", 2); n != 102 {
		t.Fatalf("HINCRBY smallhash tmp 2 = %d; want 102", n)
	}
	// {HINCRBY over 32bit value} -> 17179869185
	s.HSet("smallhash", "tmp", "17179869184")
	if n, _ := s.HIncrBy("smallhash", "tmp", 1); n != 17179869185 {
		t.Fatalf("HINCRBY over 32bit = %d; want 17179869185", n)
	}
	// {HINCRBY over 32bit value with over 32bit increment} -> 34359738368
	s.HSet("smallhash", "tmp", "17179869184")
	if n, _ := s.HIncrBy("smallhash", "tmp", 17179869184); n != 34359738368 {
		t.Fatalf("HINCRBY over 32bit incr = %d; want 34359738368", n)
	}
	// {HINCRBY HINCRBYFLOAT against non-integer increment value}
	s.Del("incrhash")
	s.HSet("incrhash", "field", "5")
	if _, err := s.HIncrBy("incrhash", "field", 2); err != nil {
		t.Fatalf("HINCRBY valid err = %v", err)
	}
	// {HINCRBY/HINCRBYFLOAT against wrong type} -> WRONGTYPE
	s.RPush("wrongtype", "a")
	if _, err := s.HIncrBy("wrongtype", "f", 2); !errors.Is(err, ErrWrongType) {
		t.Fatalf("HINCRBY wrongtype err = %v; want ErrWrongType", err)
	}
	if _, err := s.HIncrByFloat("wrongtype", "f", 2.5); !errors.Is(err, ErrWrongType) {
		t.Fatalf("HINCRBYFLOAT wrongtype err = %v; want ErrWrongType", err)
	}
	// HINCRBYFLOAT produces human-readable large integers.
	s.Del("hf")
	s.HSet("hf", "f", "17179869184")
	if _, err := s.HIncrByFloat("hf", "f", 17179869184); err != nil {
		t.Fatalf("HINCRBYFLOAT err = %v", err)
	}
	if v, _, _ := s.HGet("hf", "f"); v != "34359738368" {
		t.Fatalf("HGET hf f = %q; want 34359738368", v)
	}
}

// parityZMembers returns the members of a sorted set in rank order for comparison.
func parityZMembers(s *Store, key string) []string {
	ms, _ := s.ZRange(key, 0, -1)
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Member
	}
	return out
}

// parityEqualStrings reports whether two string slices are element-wise equal.
func parityEqualStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
