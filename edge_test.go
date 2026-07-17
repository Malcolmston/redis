package redis

import (
	"errors"
	"testing"
	"time"
)

// TestDoWrongArgs exercises the argument-count validation of every command via
// the dispatcher.
func TestDoWrongArgs(t *testing.T) {
	s := New()
	calls := [][]string{
		{"SET", "k"},
		{"GET"},
		{"GETSET", "k"},
		{"APPEND", "k"},
		{"STRLEN"},
		{"INCR"},
		{"INCRBY", "k"},
		{"DEL"},
		{"EXISTS"},
		{"EXPIRE", "k"},
		{"PEXPIRE", "k"},
		{"TTL"},
		{"PTTL"},
		{"PERSIST"},
		{"KEYS"},
		{"TYPE"},
		{"DBSIZE", "extra"},
		{"LPUSH", "k"},
		{"LPOP"},
		{"LLEN"},
		{"LINDEX", "k"},
		{"LRANGE", "k", "0"},
		{"HSET", "k", "f"},
		{"HGET", "k"},
		{"HDEL", "k"},
		{"HEXISTS", "k"},
		{"HLEN"},
		{"HKEYS"},
		{"HVALS"},
		{"HGETALL"},
		{"SADD", "k"},
		{"SREM", "k"},
		{"SISMEMBER", "k"},
		{"SCARD"},
		{"SMEMBERS"},
		{"SINTER"},
		{"ZADD", "k", "1"},
		{"ZREM", "k"},
		{"ZSCORE", "k"},
		{"ZCARD"},
		{"ZRANK", "k"},
		{"ZREVRANK", "k"},
		{"ZRANGE", "k", "0"},
		{"ZRANGEBYSCORE", "k", "0"},
	}
	for _, c := range calls {
		if _, err := s.Do(c...); !errors.Is(err, ErrWrongArgs) {
			t.Errorf("Do(%v) err = %v, want ErrWrongArgs", c, err)
		}
	}
}

// TestDoNumericParseErrors covers the non-integer/non-float argument branches.
func TestDoNumericParseErrors(t *testing.T) {
	s := New()
	_, _ = s.Do("RPUSH", "l", "a")
	bad := []struct {
		args []string
		want error
	}{
		{[]string{"INCRBY", "k", "x"}, ErrNotInteger},
		{[]string{"EXPIRE", "k", "x"}, ErrNotInteger},
		{[]string{"PEXPIRE", "k", "x"}, ErrNotInteger},
		{[]string{"LINDEX", "l", "x"}, ErrNotInteger},
		{[]string{"LRANGE", "l", "x", "1"}, ErrNotInteger},
		{[]string{"ZRANGE", "z", "x", "1"}, ErrNotInteger},
		{[]string{"ZRANGEBYSCORE", "z", "x", "1"}, ErrNotFloat},
		{[]string{"ZRANGE", "z", "0", "1", "BOGUS"}, ErrSyntax},
	}
	for _, b := range bad {
		if _, err := s.Do(b.args...); !errors.Is(err, b.want) {
			t.Errorf("Do(%v) err = %v want %v", b.args, err, b.want)
		}
	}
}

// TestDoNilReplies covers the not-found (nil) reply branches.
func TestDoNilReplies(t *testing.T) {
	s := New()
	for _, args := range [][]string{
		{"GETSET", "gs", "v"},
		{"LPOP", "l0"},
		{"RPOP", "l1"},
		{"LINDEX", "l2", "0"},
		{"HGET", "h0", "f"},
		{"ZSCORE", "z0", "m"},
		{"ZRANK", "z1", "m"},
		{"ZREVRANK", "z2", "m"},
	} {
		if r, err := s.Do(args...); err != nil || r != nil {
			t.Errorf("Do(%v) = %v,%v want nil,nil", args, r, err)
		}
	}
	// GETSET on existing returns the previous value (non-nil path already
	// covered elsewhere); here confirm it clears via a second read path.
	_, _ = s.Do("SET", "x", "1")
	if r, _ := s.Do("GETSET", "x", "2"); r != "1" {
		t.Fatalf("GETSET existing = %v", r)
	}
}

func TestManualClockSet(t *testing.T) {
	base := time.Unix(0, 0)
	clk := NewManualClock(base)
	clk.Set(base.Add(time.Hour))
	if clk.Now().Sub(base) != time.Hour {
		t.Fatalf("Set = %v", clk.Now())
	}
}

func TestPTTLMethod(t *testing.T) {
	clk := NewManualClock(time.Unix(0, 0))
	s := NewWithClock(clk)
	s.Set("k", "v", SetOptions{PX: 1500 * time.Millisecond})
	d, code := s.PTTL("k")
	if code != TTLValue || d != 1500*time.Millisecond {
		t.Fatalf("PTTL = %v %v", d, code)
	}
}

func TestRESPErrorError(t *testing.T) {
	if RESPError("boom").Error() != "boom" {
		t.Fatal("RESPError.Error")
	}
}

func TestNewWithNilClock(t *testing.T) {
	s := NewWithClock(nil)
	if !s.Set("k", "v", SetOptions{}) {
		t.Fatal("store with defaulted clock should work")
	}
}

func TestListenAndServe(t *testing.T) {
	srv := NewServer(New())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe("127.0.0.1:0") }()
	// Give it a moment to bind, then close.
	for i := 0; i < 100 && srv.Addr() == nil; i++ {
		time.Sleep(time.Millisecond)
	}
	_ = srv.Close()
	if err := <-errCh; err == nil {
		t.Fatal("ListenAndServe should return non-nil error after close")
	}
	// Binding to an invalid address should fail immediately.
	if err := srv.ListenAndServe("bad:address:zzz"); err == nil {
		t.Fatal("expected bind error")
	}
}
