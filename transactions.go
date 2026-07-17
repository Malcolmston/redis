package redis

import (
	"errors"
	"hash/fnv"
	"sort"
	"strings"
)

// ErrTxAborted is returned by Tx.Exec when a watched key changed between the
// time it was watched and the call to Exec, or when the transaction was
// discarded. It mirrors the Redis EXECABORT reply and, per Redis semantics,
// corresponds to EXEC yielding the null reply.
var ErrTxAborted = errors.New("EXECABORT transaction discarded due to WATCH")

// fingerprint is a comparable snapshot of a single key's observable state,
// captured at watch time and recomputed at Exec time to detect modification.
// Two fingerprints compare equal (with == or !=) only when the underlying key
// had the same existence, type, and contents at both sample points. The
// contents are folded into a 64-bit FNV-1a digest, so a difference in the digest
// implies a content change with overwhelming probability.
type fingerprint struct {
	// exists reports whether the key was live when the fingerprint was taken.
	exists bool
	// kind is the value type at sample time, or the zero Type for an absent key.
	kind Type
	// sum is an FNV-1a digest of the value's contents.
	sum uint64
}

// Tx is an optimistic, per-connection transaction handle over a Store. It is
// deliberately a separate object rather than state on the shared Store because
// MULTI/WATCH/EXEC have per-connection semantics that do not fit a value shared
// by many callers.
//
// Because the write paths in the rest of the package cannot be hooked to bump a
// per-key version counter, WATCH is implemented with value snapshots: at watch
// time each key's live contents are fingerprinted, and at Exec time every
// fingerprint is recomputed and compared. A key that was changed and then
// changed back to identical contents is therefore not treated as modified,
// which is acceptable for optimistic-concurrency use.
//
// Atomicity is best-effort in this embedded model. The watch re-check and the
// execution of the queued commands each acquire the Store mutex independently
// (Exec runs commands through Store.Do), so the sequence is not one atomic
// critical section. This is sufficient for optimistic concurrency control: if no
// watched key changed up to the check, the queued commands proceed, and a racing
// writer that mutates a watched key before the check causes an abort. A Tx is
// not safe for concurrent use by multiple goroutines.
type Tx struct {
	store     *Store
	queued    [][]string
	watches   map[string]fingerprint
	queueErr  error
	discarded bool
}

// Multi begins a new optimistic transaction against the Store and returns an
// empty Tx handle. Commands are accumulated with Queue and applied by Exec.
func (s *Store) Multi() *Tx {
	return &Tx{store: s, watches: make(map[string]fingerprint)}
}

// Watch records a value snapshot (fingerprint) of each named key's current live
// state, so that Exec can later detect whether any of them changed. It may be
// called more than once to extend the watch set; re-watching a key refreshes its
// snapshot. Watch returns the receiver to allow chaining.
func (t *Tx) Watch(keys ...string) *Tx {
	t.store.mu.Lock()
	defer t.store.mu.Unlock()
	for _, k := range keys {
		t.watches[k] = transactionsFingerprint(t.store.getLive(k))
	}
	return t
}

// Unwatch clears all watched keys, so a subsequent Exec performs no
// change-detection. It returns the receiver to allow chaining.
func (t *Tx) Unwatch() *Tx {
	t.watches = make(map[string]fingerprint)
	return t
}

// Queue buffers a command (name and arguments) to be run by Exec. If the command
// name is not present in the dispatch table, or no name is supplied, Queue
// records a queuing error so that Exec aborts without running anything, mirroring
// how Redis rejects an entire transaction whose commands failed to queue. Queue
// returns the receiver to allow chaining.
func (t *Tx) Queue(args ...string) *Tx {
	t.queued = append(t.queued, args)
	if len(args) == 0 {
		if t.queueErr == nil {
			t.queueErr = ErrWrongArgs
		}
		return t
	}
	cmd := strings.ToUpper(args[0])
	if _, ok := dispatchTable[cmd]; !ok {
		if t.queueErr == nil {
			t.queueErr = ErrUnknownCommand
		}
	}
	return t
}

// Discard drops the transaction, clearing all queued commands and watches. A
// discarded transaction can no longer be executed: a subsequent Exec returns
// ErrTxAborted.
func (t *Tx) Discard() {
	t.discarded = true
	t.queued = nil
	t.watches = make(map[string]fingerprint)
	t.queueErr = nil
}

// Exec runs the queued commands and returns their results. If a queuing error
// was recorded (see Queue) or the transaction was discarded, Exec runs nothing
// and returns that error, or ErrTxAborted for a discarded transaction. Otherwise
// it re-fingerprints every watched key; if any differs from its watch-time
// snapshot, Exec runs nothing and returns (nil, ErrTxAborted), corresponding to
// EXEC's null reply. When all watches still match, the queued commands are run in
// order through Store.Do and Exec returns one result per command: each element is
// the command's value, or its error if the command failed.
func (t *Tx) Exec() ([]any, error) {
	if t.discarded {
		return nil, ErrTxAborted
	}
	if t.queueErr != nil {
		return nil, t.queueErr
	}
	t.store.mu.Lock()
	for k, want := range t.watches {
		if transactionsFingerprint(t.store.getLive(k)) != want {
			t.store.mu.Unlock()
			return nil, ErrTxAborted
		}
	}
	t.store.mu.Unlock()

	results := make([]any, 0, len(t.queued))
	for _, cmd := range t.queued {
		v, err := t.store.Do(cmd...)
		if err != nil {
			results = append(results, err)
		} else {
			results = append(results, v)
		}
	}
	return results, nil
}

// transactionsFingerprint computes a fingerprint for the live item it, which may
// be nil for an absent key. The Store mutex must be held so the item's contents
// are stable during hashing. The digest is order-sensitive for lists (whose
// order is significant) and order-independent for hashes and sets (whose stored
// order is not), keeping the fingerprint stable across incidental reorderings
// while still detecting any content change.
func transactionsFingerprint(it *item) fingerprint {
	if it == nil {
		return fingerprint{}
	}
	h := fnv.New64a()
	switch it.kind {
	case TypeString:
		transactionsHashField(h, it.str)
	case TypeList:
		for _, e := range it.list {
			transactionsHashField(h, e)
		}
	case TypeHash:
		keys := make([]string, 0, len(it.hash))
		for k := range it.hash {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			transactionsHashField(h, k)
			transactionsHashField(h, it.hash[k])
		}
	case TypeSet:
		members := make([]string, 0, len(it.set))
		for m := range it.set {
			members = append(members, m)
		}
		sort.Strings(members)
		for _, m := range members {
			transactionsHashField(h, m)
		}
	case TypeZSet:
		if it.zset != nil {
			// toSlice yields members in ascending (score, member) order, which
			// is deterministic for a given set of scored members.
			for _, m := range it.zset.sl.toSlice() {
				transactionsHashField(h, m.Member)
				transactionsHashField(h, formatFloat(m.Score))
			}
		}
	}
	return fingerprint{exists: true, kind: it.kind, sum: h.Sum64()}
}

// transactionsHashField writes s to h prefixed by its length, so that adjacent
// fields cannot be confused across boundaries (for example {"a","bc"} versus
// {"ab","c"} produce distinct digests). The parameter is an inline writer
// interface to avoid importing the hash package for its Hash64 type.
func transactionsHashField(h interface{ Write([]byte) (int, error) }, s string) {
	var b [8]byte
	n := uint64(len(s))
	for i := 0; i < 8; i++ {
		b[i] = byte(n >> (8 * uint(i)))
	}
	h.Write(b[:])
	h.Write([]byte(s))
}
