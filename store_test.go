package redis

import (
	"reflect"
	"testing"
	"time"
)

func newTestStore() (*Store, *ManualClock) {
	clk := NewManualClock(time.Unix(1_700_000_000, 0))
	return NewWithClock(clk), clk
}

func TestStringBasics(t *testing.T) {
	s := New()
	if !s.Set("k", "v", SetOptions{}) {
		t.Fatal("Set should succeed")
	}
	if v, ok, err := s.Get("k"); err != nil || !ok || v != "v" {
		t.Fatalf("Get = %q,%v,%v", v, ok, err)
	}
	if _, ok, _ := s.Get("missing"); ok {
		t.Fatal("missing key should not be ok")
	}
	// APPEND creates and extends.
	if n, _ := s.Append("app", "ab"); n != 2 {
		t.Fatalf("append len = %d", n)
	}
	if n, _ := s.Append("app", "cd"); n != 4 {
		t.Fatalf("append len = %d", n)
	}
	if n, _ := s.Strlen("app"); n != 4 {
		t.Fatalf("strlen = %d", n)
	}
	if n, _ := s.Strlen("nope"); n != 0 {
		t.Fatalf("strlen missing = %d", n)
	}
}

func TestGetSet(t *testing.T) {
	s := New()
	if _, had, _ := s.GetSet("k", "1"); had {
		t.Fatal("first GetSet should report no previous value")
	}
	if old, had, _ := s.GetSet("k", "2"); !had || old != "1" {
		t.Fatalf("GetSet old = %q had=%v", old, had)
	}
}

func TestSetNXXX(t *testing.T) {
	s := New()
	if !s.Set("k", "1", SetOptions{NX: true}) {
		t.Fatal("NX on missing key should set")
	}
	if s.Set("k", "2", SetOptions{NX: true}) {
		t.Fatal("NX on existing key should fail")
	}
	if !s.Set("k", "3", SetOptions{XX: true}) {
		t.Fatal("XX on existing key should set")
	}
	if s.Set("other", "x", SetOptions{XX: true}) {
		t.Fatal("XX on missing key should fail")
	}
	if v, _, _ := s.Get("k"); v != "3" {
		t.Fatalf("value = %q", v)
	}
}

func TestIncrDecr(t *testing.T) {
	s := New()
	if n, err := s.Incr("c"); err != nil || n != 1 {
		t.Fatalf("incr = %d %v", n, err)
	}
	if n, _ := s.IncrBy("c", 9); n != 10 {
		t.Fatalf("incrby = %d", n)
	}
	if n, _ := s.Decr("c"); n != 9 {
		t.Fatalf("decr = %d", n)
	}
	if n, _ := s.DecrBy("c", 4); n != 5 {
		t.Fatalf("decrby = %d", n)
	}
	s.Set("bad", "notint", SetOptions{})
	if _, err := s.Incr("bad"); err != ErrNotInteger {
		t.Fatalf("expected ErrNotInteger, got %v", err)
	}
}

func TestWrongType(t *testing.T) {
	s := New()
	_, _ = s.LPush("l", "a")
	if _, _, err := s.Get("l"); err != ErrWrongType {
		t.Fatalf("expected wrongtype, got %v", err)
	}
	if _, err := s.SAdd("l", "x"); err != ErrWrongType {
		t.Fatalf("expected wrongtype, got %v", err)
	}
}

func TestDelExistsTypeDBSize(t *testing.T) {
	s := New()
	s.Set("a", "1", SetOptions{})
	s.Set("b", "2", SetOptions{})
	if n := s.Exists("a", "b", "a", "z"); n != 3 {
		t.Fatalf("exists = %d", n)
	}
	if s.TypeOf("a") != TypeString {
		t.Fatal("type a")
	}
	if s.TypeOf("z") != TypeNone {
		t.Fatal("type z")
	}
	if s.DBSize() != 2 {
		t.Fatal("dbsize")
	}
	if n := s.Del("a", "z"); n != 1 {
		t.Fatalf("del = %d", n)
	}
	s.FlushAll()
	if s.DBSize() != 0 {
		t.Fatal("flushall")
	}
}

func TestExpiryDeterministic(t *testing.T) {
	s, clk := newTestStore()
	s.Set("k", "v", SetOptions{})
	if !s.Expire("k", 10*time.Second) {
		t.Fatal("expire should succeed")
	}
	d, code := s.TTL("k")
	if code != TTLValue || d != 10*time.Second {
		t.Fatalf("ttl = %v %v", d, code)
	}
	clk.Advance(9 * time.Second)
	if _, ok, _ := s.Get("k"); !ok {
		t.Fatal("key should still be alive")
	}
	clk.Advance(1 * time.Second) // now exactly at expiry
	if _, ok, _ := s.Get("k"); ok {
		t.Fatal("key should be expired at expireAt")
	}
	if _, code := s.TTL("k"); code != TTLNoKey {
		t.Fatalf("ttl code after expiry = %v", code)
	}
}

func TestExpirePersistAndNoKey(t *testing.T) {
	s, _ := newTestStore()
	if s.Expire("missing", time.Second) {
		t.Fatal("expire on missing key should fail")
	}
	s.Set("k", "v", SetOptions{})
	if _, code := s.TTL("k"); code != TTLNoExpiry {
		t.Fatalf("ttl no expiry code = %v", code)
	}
	s.Expire("k", 5*time.Second)
	if !s.Persist("k") {
		t.Fatal("persist should clear ttl")
	}
	if s.Persist("k") {
		t.Fatal("persist without ttl should return false")
	}
	// Negative expire deletes.
	s.Set("d", "v", SetOptions{})
	if !s.Expire("d", -1) {
		t.Fatal("negative expire returns true")
	}
	if s.Exists("d") != 0 {
		t.Fatal("key should be deleted")
	}
}

func TestSetWithEXPX(t *testing.T) {
	s, clk := newTestStore()
	s.Set("k", "v", SetOptions{EX: 2 * time.Second})
	clk.Advance(1 * time.Second)
	if _, ok, _ := s.Get("k"); !ok {
		t.Fatal("should live")
	}
	clk.Advance(2 * time.Second)
	if _, ok, _ := s.Get("k"); ok {
		t.Fatal("should expire")
	}
	s.Set("p", "v", SetOptions{PX: 500 * time.Millisecond})
	clk.Advance(500 * time.Millisecond)
	if s.Exists("p") != 0 {
		t.Fatal("px should expire")
	}
}

func TestKeysGlobAndExpiredSkipped(t *testing.T) {
	s, clk := newTestStore()
	s.Set("user:1", "a", SetOptions{})
	s.Set("user:2", "b", SetOptions{})
	s.Set("post:1", "c", SetOptions{})
	s.Set("temp", "t", SetOptions{EX: time.Second})
	got := s.Keys("user:*")
	if len(got) != 2 {
		t.Fatalf("keys user:* = %v", got)
	}
	if len(s.Keys("*")) != 4 {
		t.Fatal("keys *")
	}
	clk.Advance(2 * time.Second)
	if len(s.Keys("*")) != 3 {
		t.Fatal("expired key should be skipped by KEYS")
	}
	if s.DBSize() != 3 {
		t.Fatal("dbsize should skip expired")
	}
}

func TestLists(t *testing.T) {
	s := New()
	_, _ = s.RPush("l", "b", "c")
	_, _ = s.LPush("l", "a") // a b c
	if n, _ := s.LLen("l"); n != 3 {
		t.Fatalf("llen = %d", n)
	}
	if v, ok, _ := s.LIndex("l", 0); !ok || v != "a" {
		t.Fatalf("lindex 0 = %q", v)
	}
	if v, ok, _ := s.LIndex("l", -1); !ok || v != "c" {
		t.Fatalf("lindex -1 = %q", v)
	}
	if _, ok, _ := s.LIndex("l", 99); ok {
		t.Fatal("out of range index")
	}
	rng, _ := s.LRange("l", 0, -1)
	if !reflect.DeepEqual(rng, []string{"a", "b", "c"}) {
		t.Fatalf("lrange = %v", rng)
	}
	rng, _ = s.LRange("l", 1, 1)
	if !reflect.DeepEqual(rng, []string{"b"}) {
		t.Fatalf("lrange 1 1 = %v", rng)
	}
	if v, ok, _ := s.LPop("l"); !ok || v != "a" {
		t.Fatalf("lpop = %q", v)
	}
	if v, ok, _ := s.RPop("l"); !ok || v != "c" {
		t.Fatalf("rpop = %q", v)
	}
	_, _, _ = s.LPop("l") // now empty -> deleted
	if s.Exists("l") != 0 {
		t.Fatal("empty list should be deleted")
	}
	if _, ok, _ := s.LPop("gone"); ok {
		t.Fatal("pop missing")
	}
}

func TestHashes(t *testing.T) {
	s := New()
	if n, _ := s.HSet("h", "f1", "v1", "f2", "v2"); n != 2 {
		t.Fatalf("hset added = %d", n)
	}
	if n, _ := s.HSet("h", "f1", "x"); n != 0 {
		t.Fatalf("hset update added = %d", n)
	}
	if v, ok, _ := s.HGet("h", "f1"); !ok || v != "x" {
		t.Fatalf("hget = %q", v)
	}
	if ok, _ := s.HExists("h", "f2"); !ok {
		t.Fatal("hexists")
	}
	if n, _ := s.HLen("h"); n != 2 {
		t.Fatalf("hlen = %d", n)
	}
	if k, _ := s.HKeys("h"); !reflect.DeepEqual(k, []string{"f1", "f2"}) {
		t.Fatalf("hkeys = %v", k)
	}
	if v, _ := s.HVals("h"); !reflect.DeepEqual(v, []string{"x", "v2"}) {
		t.Fatalf("hvals = %v", v)
	}
	all, _ := s.HGetAll("h")
	if !reflect.DeepEqual(all, map[string]string{"f1": "x", "f2": "v2"}) {
		t.Fatalf("hgetall = %v", all)
	}
	if n, _ := s.HDel("h", "f1", "nope"); n != 1 {
		t.Fatalf("hdel = %d", n)
	}
	_, _ = s.HDel("h", "f2")
	if s.Exists("h") != 0 {
		t.Fatal("empty hash deleted")
	}
	if _, err := s.HSet("h", "odd"); err != ErrWrongArgs {
		t.Fatalf("odd hset args: %v", err)
	}
}

func TestSets(t *testing.T) {
	s := New()
	if n, _ := s.SAdd("s1", "a", "b", "c", "a"); n != 3 {
		t.Fatalf("sadd = %d", n)
	}
	_, _ = s.SAdd("s2", "b", "c", "d")
	if n, _ := s.SCard("s1"); n != 3 {
		t.Fatalf("scard = %d", n)
	}
	if ok, _ := s.SIsMember("s1", "a"); !ok {
		t.Fatal("sismember a")
	}
	if ok, _ := s.SIsMember("s1", "z"); ok {
		t.Fatal("sismember z")
	}
	if m, _ := s.SMembers("s1"); !reflect.DeepEqual(m, []string{"a", "b", "c"}) {
		t.Fatalf("smembers = %v", m)
	}
	if m, _ := s.SInter("s1", "s2"); !reflect.DeepEqual(m, []string{"b", "c"}) {
		t.Fatalf("sinter = %v", m)
	}
	if m, _ := s.SUnion("s1", "s2"); !reflect.DeepEqual(m, []string{"a", "b", "c", "d"}) {
		t.Fatalf("sunion = %v", m)
	}
	if m, _ := s.SDiff("s1", "s2"); !reflect.DeepEqual(m, []string{"a"}) {
		t.Fatalf("sdiff = %v", m)
	}
	if n, _ := s.SRem("s1", "a", "x"); n != 1 {
		t.Fatalf("srem = %d", n)
	}
	// Intersection with missing key is empty.
	if m, _ := s.SInter("s1", "missing"); len(m) != 0 {
		t.Fatalf("sinter missing = %v", m)
	}
}

func TestSortedSet(t *testing.T) {
	s := New()
	n, _ := s.ZAdd("z",
		ZMember{"a", 1}, ZMember{"b", 2}, ZMember{"c", 3}, ZMember{"d", 2})
	if n != 4 {
		t.Fatalf("zadd = %d", n)
	}
	// Update existing does not count.
	if n, _ := s.ZAdd("z", ZMember{"a", 5}); n != 0 {
		t.Fatalf("zadd update = %d", n)
	}
	if sc, ok, _ := s.ZScore("z", "a"); !ok || sc != 5 {
		t.Fatalf("zscore = %v", sc)
	}
	if c, _ := s.ZCard("z"); c != 4 {
		t.Fatalf("zcard = %d", c)
	}
	// Order now: b(2) d(2) c(3) a(5). Ties broken lexically: b<d.
	fwd, _ := s.ZRange("z", 0, -1)
	wantOrder := []string{"b", "d", "c", "a"}
	for i, m := range fwd {
		if m.Member != wantOrder[i] {
			t.Fatalf("zrange order = %v", fwd)
		}
	}
	if r, ok, _ := s.ZRank("z", "c"); !ok || r != 2 {
		t.Fatalf("zrank c = %d", r)
	}
	if r, ok, _ := s.ZRevRank("z", "c"); !ok || r != 1 {
		t.Fatalf("zrevrank c = %d", r)
	}
	rev, _ := s.ZRevRange("z", 0, 1)
	if rev[0].Member != "a" || rev[1].Member != "c" {
		t.Fatalf("zrevrange = %v", rev)
	}
	byScore, _ := s.ZRangeByScore("z", ScoreRange{Min: 2, Max: 3})
	if len(byScore) != 3 {
		t.Fatalf("zrangebyscore = %v", byScore)
	}
	// Exclusive bounds.
	ex, _ := s.ZRangeByScore("z", ScoreRange{Min: 2, Max: 5, MinExclusive: true, MaxExclusive: true})
	if len(ex) != 1 || ex[0].Member != "c" {
		t.Fatalf("zrangebyscore exclusive = %v", ex)
	}
	if n, _ := s.ZRem("z", "a", "b"); n != 2 {
		t.Fatalf("zrem = %d", n)
	}
	if _, ok, _ := s.ZRank("z", "a"); ok {
		t.Fatal("removed member should have no rank")
	}
}

func TestSortedSetRankLargeAndReorder(t *testing.T) {
	s := New()
	for i := 0; i < 200; i++ {
		_, _ = s.ZAdd("z", ZMember{Member: string(rune('A' + i%26)), Score: float64(i)})
	}
	// Re-add same members with new scores to exercise reordering paths.
	_, _ = s.ZAdd("z", ZMember{"A", 1000})
	if sc, _, _ := s.ZScore("z", "A"); sc != 1000 {
		t.Fatalf("reorder score = %v", sc)
	}
	all, _ := s.ZRange("z", 0, -1)
	// Verify ascending order holds.
	for i := 1; i < len(all); i++ {
		if all[i-1].Score > all[i].Score {
			t.Fatalf("not sorted at %d: %v", i, all)
		}
	}
	// Last element must be A with score 1000.
	if all[len(all)-1].Member != "A" {
		t.Fatalf("expected A last, got %v", all[len(all)-1])
	}
}
