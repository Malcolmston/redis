package redis

import (
	"errors"
	"reflect"
	"testing"
)

// mutate applies a function to the store, failing the test on error.
func txMustDo(t *testing.T, s *Store, args ...string) any {
	t.Helper()
	v, err := s.Do(args...)
	if err != nil {
		t.Fatalf("Do(%v): unexpected error: %v", args, err)
	}
	return v
}

func TestTxExecSuccess(t *testing.T) {
	s := New()
	tx := s.Multi().
		Queue("SET", "k", "v").
		Queue("APPEND", "k", "!").
		Queue("GET", "k")
	res, err := tx.Exec()
	if err != nil {
		t.Fatalf("Exec: unexpected error: %v", err)
	}
	want := []any{SimpleString("OK"), int64(2), "v!"}
	if !reflect.DeepEqual(res, want) {
		t.Fatalf("results = %#v, want %#v", res, want)
	}
	if got := txMustDo(t, s, "GET", "k"); got != "v!" {
		t.Fatalf("final GET = %#v, want %q", got, "v!")
	}
}

func TestTxQueueErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want error
	}{
		{"unknown command", []string{"NOPE", "x"}, ErrUnknownCommand},
		{"empty command", nil, ErrWrongArgs},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			tx := s.Multi().Queue("SET", "a", "1").Queue(tc.args...)
			res, err := tx.Exec()
			if res != nil {
				t.Fatalf("results = %#v, want nil", res)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			// The valid command must not have executed.
			if n := s.Exists("a"); n != 0 {
				t.Fatalf("key a exists = %d, want 0 (transaction must not run)", n)
			}
		})
	}
}

func TestTxCommandErrorCaptured(t *testing.T) {
	s := New()
	txMustDo(t, s, "SET", "s", "notalist")
	// LPUSH against a string yields a per-command WRONGTYPE error, captured as a
	// result element while the transaction as a whole still succeeds.
	res, err := s.Multi().
		Queue("SET", "n", "1").
		Queue("LPUSH", "s", "x").
		Queue("INCR", "n").
		Exec()
	if err != nil {
		t.Fatalf("Exec: unexpected error: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("len(res) = %d, want 3", len(res))
	}
	if res[0] != SimpleString("OK") {
		t.Fatalf("res[0] = %#v, want OK", res[0])
	}
	perCmdErr, ok := res[1].(error)
	if !ok || !errors.Is(perCmdErr, ErrWrongType) {
		t.Fatalf("res[1] = %#v, want WRONGTYPE error", res[1])
	}
	if res[2] != int64(2) {
		t.Fatalf("res[2] = %#v, want int64(2)", res[2])
	}
}

func TestTxWatchUnchanged(t *testing.T) {
	s := New()
	txMustDo(t, s, "SET", "w", "orig")
	tx := s.Multi()
	tx.Watch("w")
	tx.Queue("APPEND", "w", "-x")
	res, err := tx.Exec()
	if err != nil {
		t.Fatalf("Exec: unexpected error: %v", err)
	}
	want := []any{int64(6)}
	if !reflect.DeepEqual(res, want) {
		t.Fatalf("results = %#v, want %#v", res, want)
	}
}

func TestTxWatchChangedAborts(t *testing.T) {
	type kind struct {
		name  string
		setup []string
		bump  []string // mutation applied after Watch
	}
	tests := []kind{
		{"string", []string{"SET", "k", "v"}, []string{"APPEND", "k", "z"}},
		{"string-delete", []string{"SET", "k", "v"}, []string{"DEL", "k"}},
		{"list", []string{"RPUSH", "k", "a"}, []string{"RPUSH", "k", "b"}},
		{"hash", []string{"HSET", "k", "f", "1"}, []string{"HSET", "k", "g", "2"}},
		{"set", []string{"SADD", "k", "a"}, []string{"SADD", "k", "b"}},
		{"zset", []string{"ZADD", "k", "1", "a"}, []string{"ZADD", "k", "2", "b"}},
		{"zset-score", []string{"ZADD", "k", "1", "a"}, []string{"ZADD", "k", "5", "a"}},
		{"create", nil, []string{"SET", "k", "new"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			if tc.setup != nil {
				txMustDo(t, s, tc.setup...)
			}
			tx := s.Multi()
			tx.Watch("k")
			tx.Queue("SET", "sentinel", "ran")
			// Concurrent-style mutation of the watched key.
			txMustDo(t, s, tc.bump...)
			res, err := tx.Exec()
			if !errors.Is(err, ErrTxAborted) {
				t.Fatalf("err = %v, want ErrTxAborted", err)
			}
			if res != nil {
				t.Fatalf("results = %#v, want nil", res)
			}
			if n := s.Exists("sentinel"); n != 0 {
				t.Fatalf("sentinel exists = %d, want 0 (queued command must not run)", n)
			}
		})
	}
}

func TestTxWatchNoChangeVariants(t *testing.T) {
	// Fingerprints must be stable when nothing meaningful changes: re-adding an
	// existing set member, or setting a hash field to its current value.
	tests := []struct {
		name  string
		setup []string
		noop  []string
	}{
		{"set re-add", []string{"SADD", "k", "a", "b"}, []string{"SADD", "k", "a"}},
		{"hash same value", []string{"HSET", "k", "f", "1"}, []string{"HSET", "k", "f", "1"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			txMustDo(t, s, tc.setup...)
			tx := s.Multi()
			tx.Watch("k")
			tx.Queue("SET", "ok", "1")
			txMustDo(t, s, tc.noop...)
			res, err := tx.Exec()
			if err != nil {
				t.Fatalf("Exec: unexpected error: %v", err)
			}
			if !reflect.DeepEqual(res, []any{SimpleString("OK")}) {
				t.Fatalf("results = %#v, want [OK]", res)
			}
		})
	}
}

func TestTxUnwatch(t *testing.T) {
	s := New()
	txMustDo(t, s, "SET", "k", "v")
	tx := s.Multi()
	tx.Watch("k")
	tx.Unwatch()
	tx.Queue("GET", "k")
	// Change the key after unwatching: Exec must still run.
	txMustDo(t, s, "APPEND", "k", "z")
	res, err := tx.Exec()
	if err != nil {
		t.Fatalf("Exec: unexpected error: %v", err)
	}
	if !reflect.DeepEqual(res, []any{"vz"}) {
		t.Fatalf("results = %#v, want [\"vz\"]", res)
	}
}

func TestTxDiscard(t *testing.T) {
	s := New()
	tx := s.Multi().Queue("SET", "k", "v")
	tx.Discard()
	res, err := tx.Exec()
	if !errors.Is(err, ErrTxAborted) {
		t.Fatalf("err = %v, want ErrTxAborted", err)
	}
	if res != nil {
		t.Fatalf("results = %#v, want nil", res)
	}
	if n := s.Exists("k"); n != 0 {
		t.Fatalf("key k exists = %d, want 0", n)
	}
}

func TestTxFingerprint(t *testing.T) {
	// transactionsFingerprint distinguishes types and contents, and treats an
	// absent key as a distinct, stable value.
	s := New()
	absent := transactionsFingerprint(s.getLive("missing"))
	if absent.exists {
		t.Fatalf("absent fingerprint.exists = true, want false")
	}
	if absent != transactionsFingerprint(s.getLive("missing2")) {
		t.Fatalf("two absent fingerprints must be equal")
	}

	txMustDo(t, s, "SET", "str", "abc")
	txMustDo(t, s, "RPUSH", "lst", "abc")
	fpStr := transactionsFingerprint(s.getLive("str"))
	fpLst := transactionsFingerprint(s.getLive("lst"))
	if fpStr.kind != TypeString || fpLst.kind != TypeList {
		t.Fatalf("kinds = %v, %v", fpStr.kind, fpLst.kind)
	}
	if fpStr == fpLst {
		t.Fatalf("string and list with same element must differ by kind")
	}
	if fpStr != transactionsFingerprint(s.getLive("str")) {
		t.Fatalf("fingerprint of unchanged string must be stable")
	}
}

func TestTxFingerprintFieldBoundaries(t *testing.T) {
	// Length-prefixing must keep {"a","bc"} distinct from {"ab","c"}.
	s := New()
	txMustDo(t, s, "RPUSH", "l1", "a", "bc")
	txMustDo(t, s, "RPUSH", "l2", "ab", "c")
	if transactionsFingerprint(s.getLive("l1")) == transactionsFingerprint(s.getLive("l2")) {
		t.Fatalf("field boundaries must not collide")
	}
}
