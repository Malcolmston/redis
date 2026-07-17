package redis

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestDoStrings(t *testing.T) {
	s := New()
	if r, err := s.Do("SET", "k", "v"); err != nil || r != SimpleString("OK") {
		t.Fatalf("SET = %v %v", r, err)
	}
	if r, _ := s.Do("GET", "k"); r != "v" {
		t.Fatalf("GET = %v", r)
	}
	if r, _ := s.Do("GET", "missing"); r != nil {
		t.Fatalf("GET missing = %v", r)
	}
	if r, _ := s.Do("SET", "n", "1"); r != SimpleString("OK") {
		t.Fatal("set n")
	}
	if r, _ := s.Do("INCR", "n"); r != int64(2) {
		t.Fatalf("INCR = %v", r)
	}
	if r, _ := s.Do("INCRBY", "n", "5"); r != int64(7) {
		t.Fatalf("INCRBY = %v", r)
	}
	if r, _ := s.Do("DECR", "n"); r != int64(6) {
		t.Fatalf("DECR = %v", r)
	}
	if r, _ := s.Do("DECRBY", "n", "2"); r != int64(4) {
		t.Fatalf("DECRBY = %v", r)
	}
	if r, _ := s.Do("APPEND", "k", "2"); r != int64(2) {
		t.Fatalf("APPEND = %v", r)
	}
	if r, _ := s.Do("STRLEN", "k"); r != int64(2) {
		t.Fatalf("STRLEN = %v", r)
	}
	if r, _ := s.Do("GETSET", "k", "z"); r != "v2" {
		t.Fatalf("GETSET = %v", r)
	}
	if r, _ := s.Do("TYPE", "k"); r != SimpleString("string") {
		t.Fatalf("TYPE = %v", r)
	}
}

func TestDoSetOptions(t *testing.T) {
	s := New()
	if r, _ := s.Do("SET", "k", "v", "NX"); r != SimpleString("OK") {
		t.Fatal("SET NX new")
	}
	if r, _ := s.Do("SET", "k", "v2", "NX"); r != nil {
		t.Fatalf("SET NX existing = %v", r)
	}
	if r, _ := s.Do("SET", "k", "v3", "XX"); r != SimpleString("OK") {
		t.Fatal("SET XX")
	}
	if _, err := s.Do("SET", "k", "v", "EX", "x"); !errors.Is(err, ErrNotInteger) {
		t.Fatalf("SET EX bad = %v", err)
	}
	if _, err := s.Do("SET", "k", "v", "NX", "XX"); !errors.Is(err, ErrSyntax) {
		t.Fatalf("SET NX XX = %v", err)
	}
	if _, err := s.Do("SET", "k", "v", "BOGUS"); !errors.Is(err, ErrSyntax) {
		t.Fatalf("SET bogus = %v", err)
	}
	if r, _ := s.Do("SET", "e", "v", "EX", "100"); r != SimpleString("OK") {
		t.Fatal("SET EX ok")
	}
	if r, _ := s.Do("TTL", "e"); r != int64(100) {
		t.Fatalf("TTL = %v", r)
	}
}

func TestDoExpiry(t *testing.T) {
	clk := NewManualClock(time.Unix(1000, 0))
	s := NewWithClock(clk)
	_, _ = s.Do("SET", "k", "v")
	if r, _ := s.Do("TTL", "k"); r != int64(-1) {
		t.Fatalf("TTL no expiry = %v", r)
	}
	if r, _ := s.Do("TTL", "missing"); r != int64(-2) {
		t.Fatalf("TTL missing = %v", r)
	}
	if r, _ := s.Do("EXPIRE", "k", "10"); r != int64(1) {
		t.Fatalf("EXPIRE = %v", r)
	}
	if r, _ := s.Do("PTTL", "k"); r != int64(10000) {
		t.Fatalf("PTTL = %v", r)
	}
	if r, _ := s.Do("PERSIST", "k"); r != int64(1) {
		t.Fatalf("PERSIST = %v", r)
	}
	_, _ = s.Do("PEXPIRE", "k", "500")
	clk.Advance(500 * time.Millisecond)
	if r, _ := s.Do("EXISTS", "k"); r != int64(0) {
		t.Fatalf("EXISTS after expiry = %v", r)
	}
}

func TestDoListsHashesSets(t *testing.T) {
	s := New()
	_, _ = s.Do("RPUSH", "l", "a", "b", "c")
	_, _ = s.Do("LPUSH", "l", "z")
	if r, _ := s.Do("LLEN", "l"); r != int64(4) {
		t.Fatalf("LLEN = %v", r)
	}
	if r, _ := s.Do("LINDEX", "l", "0"); r != "z" {
		t.Fatalf("LINDEX = %v", r)
	}
	r, _ := s.Do("LRANGE", "l", "0", "-1")
	if !reflect.DeepEqual(r, []any{"z", "a", "b", "c"}) {
		t.Fatalf("LRANGE = %v", r)
	}
	if r, _ := s.Do("LPOP", "l"); r != "z" {
		t.Fatalf("LPOP = %v", r)
	}
	if r, _ := s.Do("RPOP", "l"); r != "c" {
		t.Fatalf("RPOP = %v", r)
	}

	_, _ = s.Do("HSET", "h", "f1", "v1", "f2", "v2")
	if r, _ := s.Do("HGET", "h", "f1"); r != "v1" {
		t.Fatalf("HGET = %v", r)
	}
	if r, _ := s.Do("HLEN", "h"); r != int64(2) {
		t.Fatalf("HLEN = %v", r)
	}
	if r, _ := s.Do("HEXISTS", "h", "f2"); r != int64(1) {
		t.Fatalf("HEXISTS = %v", r)
	}
	r, _ = s.Do("HKEYS", "h")
	if !reflect.DeepEqual(r, []any{"f1", "f2"}) {
		t.Fatalf("HKEYS = %v", r)
	}
	r, _ = s.Do("HVALS", "h")
	if !reflect.DeepEqual(r, []any{"v1", "v2"}) {
		t.Fatalf("HVALS = %v", r)
	}
	r, _ = s.Do("HGETALL", "h")
	if !reflect.DeepEqual(r, []any{"f1", "v1", "f2", "v2"}) {
		t.Fatalf("HGETALL = %v", r)
	}
	if r, _ := s.Do("HDEL", "h", "f1"); r != int64(1) {
		t.Fatalf("HDEL = %v", r)
	}

	_, _ = s.Do("SADD", "s1", "a", "b", "c")
	_, _ = s.Do("SADD", "s2", "b", "c", "d")
	if r, _ := s.Do("SCARD", "s1"); r != int64(3) {
		t.Fatalf("SCARD = %v", r)
	}
	if r, _ := s.Do("SISMEMBER", "s1", "a"); r != int64(1) {
		t.Fatalf("SISMEMBER = %v", r)
	}
	r, _ = s.Do("SMEMBERS", "s1")
	if !reflect.DeepEqual(r, []any{"a", "b", "c"}) {
		t.Fatalf("SMEMBERS = %v", r)
	}
	r, _ = s.Do("SINTER", "s1", "s2")
	if !reflect.DeepEqual(r, []any{"b", "c"}) {
		t.Fatalf("SINTER = %v", r)
	}
	r, _ = s.Do("SUNION", "s1", "s2")
	if !reflect.DeepEqual(r, []any{"a", "b", "c", "d"}) {
		t.Fatalf("SUNION = %v", r)
	}
	r, _ = s.Do("SDIFF", "s1", "s2")
	if !reflect.DeepEqual(r, []any{"a"}) {
		t.Fatalf("SDIFF = %v", r)
	}
	if r, _ := s.Do("SREM", "s1", "a"); r != int64(1) {
		t.Fatalf("SREM = %v", r)
	}
}

func TestDoSortedSets(t *testing.T) {
	s := New()
	if r, _ := s.Do("ZADD", "z", "1", "a", "2", "b", "3", "c"); r != int64(3) {
		t.Fatalf("ZADD = %v", r)
	}
	if r, _ := s.Do("ZCARD", "z"); r != int64(3) {
		t.Fatalf("ZCARD = %v", r)
	}
	if r, _ := s.Do("ZSCORE", "z", "b"); r != "2" {
		t.Fatalf("ZSCORE = %v", r)
	}
	if r, _ := s.Do("ZRANK", "z", "c"); r != int64(2) {
		t.Fatalf("ZRANK = %v", r)
	}
	if r, _ := s.Do("ZREVRANK", "z", "c"); r != int64(0) {
		t.Fatalf("ZREVRANK = %v", r)
	}
	r, _ := s.Do("ZRANGE", "z", "0", "-1")
	if !reflect.DeepEqual(r, []any{"a", "b", "c"}) {
		t.Fatalf("ZRANGE = %v", r)
	}
	r, _ = s.Do("ZRANGE", "z", "0", "-1", "WITHSCORES")
	if !reflect.DeepEqual(r, []any{"a", "1", "b", "2", "c", "3"}) {
		t.Fatalf("ZRANGE WITHSCORES = %v", r)
	}
	r, _ = s.Do("ZREVRANGE", "z", "0", "1")
	if !reflect.DeepEqual(r, []any{"c", "b"}) {
		t.Fatalf("ZREVRANGE = %v", r)
	}
	r, _ = s.Do("ZRANGEBYSCORE", "z", "2", "+inf")
	if !reflect.DeepEqual(r, []any{"b", "c"}) {
		t.Fatalf("ZRANGEBYSCORE = %v", r)
	}
	r, _ = s.Do("ZRANGEBYSCORE", "z", "(1", "3")
	if !reflect.DeepEqual(r, []any{"b", "c"}) {
		t.Fatalf("ZRANGEBYSCORE exclusive = %v", r)
	}
	if r, _ := s.Do("ZREM", "z", "a"); r != int64(1) {
		t.Fatalf("ZREM = %v", r)
	}
}

func TestDoGenericAndErrors(t *testing.T) {
	s := New()
	_, _ = s.Do("SET", "a", "1")
	_, _ = s.Do("SET", "b", "2")
	r, _ := s.Do("KEYS", "*")
	if len(r.([]any)) != 2 {
		t.Fatalf("KEYS = %v", r)
	}
	if r, _ := s.Do("DBSIZE"); r != int64(2) {
		t.Fatalf("DBSIZE = %v", r)
	}
	if r, _ := s.Do("DEL", "a", "b"); r != int64(2) {
		t.Fatalf("DEL = %v", r)
	}
	if r, _ := s.Do("FLUSHALL"); r != SimpleString("OK") {
		t.Fatal("FLUSHALL")
	}

	if _, err := s.Do(); !errors.Is(err, ErrWrongArgs) {
		t.Fatalf("empty Do = %v", err)
	}
	if _, err := s.Do("NOPE"); !errors.Is(err, ErrUnknownCommand) {
		t.Fatalf("unknown = %v", err)
	}
	if _, err := s.Do("GET"); !errors.Is(err, ErrWrongArgs) {
		t.Fatalf("GET no args = %v", err)
	}
	if _, err := s.Do("ZADD", "z", "notfloat", "m"); !errors.Is(err, ErrNotFloat) {
		t.Fatalf("ZADD bad float = %v", err)
	}
	// Wrong type surfaces through Do.
	_, _ = s.Do("LPUSH", "l", "x")
	if _, err := s.Do("GET", "l"); !errors.Is(err, ErrWrongType) {
		t.Fatalf("wrongtype via Do = %v", err)
	}
}
