package redis

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// This file implements Redis Streams (the XADD/XRANGE/XREAD family) as a
// self-contained feature area layered on top of the existing package.
//
// The fixed item struct in store.go has no field for stream data and existing
// files may not be edited, so all stream state is held in a package-level
// registry (streamReg) keyed by the owning *Store. Streams therefore live in
// their own namespace, completely independent of Store.data: a stream stored at
// the key "mystream" and an ordinary value (string, list, hash, ...) stored at
// the Store.data key "mystream" are two entirely distinct objects that do not
// alias, shadow, or collide with one another. Deleting one via Del has no
// effect on the other, and TypeOf never reports a stream.
//
// All time-derived values (the millisecond component of auto-generated IDs and
// pending-entry idle times) come from the Store's injected Clock via
// s.clock.Now(), so tests using a ManualClock are fully deterministic.

// streamState is the unexported per-key stream container held in streamReg. It
// stores entries in ascending ID order, the last generated (or explicitly
// added) ID, and any consumer groups defined on the stream.
type streamState struct {
	entries []StreamEntry
	lastID  StreamID
	groups  map[string]*streamGroup
}

// streamGroup is a consumer group: it remembers the last ID it delivered to any
// consumer and holds the pending-entries list (PEL) of delivered-but-unacked
// entries keyed by entry ID.
type streamGroup struct {
	lastDelivered StreamID
	pending       map[StreamID]*streamPending
}

// streamPending is one entry in a consumer group's pending-entries list.
type streamPending struct {
	consumer    string
	deliveries  int64
	deliveredMs int64
}

// streamReg is the package-level registry that holds all stream state, keyed by
// the owning *Store and then by stream key. It carries its own mutex so stream
// operations never contend with (or depend on) the Store's own data lock.
var streamReg = struct {
	mu sync.Mutex
	m  map[*Store]map[string]*streamState
}{m: map[*Store]map[string]*streamState{}}

// streamsStateFor returns the streamState for (s, key). When create is true a
// missing store map or stream is allocated; otherwise a missing stream yields
// nil. The caller must hold streamReg.mu.
func streamsStateFor(s *Store, key string, create bool) *streamState {
	m := streamReg.m[s]
	if m == nil {
		if !create {
			return nil
		}
		m = map[string]*streamState{}
		streamReg.m[s] = m
	}
	st := m[key]
	if st == nil && create {
		st = &streamState{groups: map[string]*streamGroup{}}
		m[key] = st
	}
	return st
}

// streamsHasLast reports whether the stream has ever had an ID assigned. Because
// valid IDs are always greater than 0-0, a zero lastID means "no ID yet" even
// after every entry has been deleted (deletion does not reset lastID).
func streamsHasLast(st *streamState) bool {
	return st.lastID.Ms != 0 || st.lastID.Seq != 0
}

// streamsFindEntry returns the entry with the given ID and whether it was found.
// The caller must hold streamReg.mu.
func streamsFindEntry(st *streamState, id StreamID) (StreamEntry, bool) {
	for _, e := range st.entries {
		if e.ID.Compare(id) == 0 {
			return e, true
		}
	}
	return StreamEntry{}, false
}

// StreamID is a stream entry identifier composed of a millisecond timestamp and
// a per-millisecond sequence number, rendered as "<Ms>-<Seq>".
type StreamID struct {
	// Ms is the millisecond timestamp component of the ID.
	Ms uint64
	// Seq is the sequence number distinguishing IDs sharing the same Ms.
	Seq uint64
}

// String returns the canonical "<ms>-<seq>" text form of the ID.
func (id StreamID) String() string {
	return strconv.FormatUint(id.Ms, 10) + "-" + strconv.FormatUint(id.Seq, 10)
}

// Compare orders two IDs, returning -1 if id sorts before o, 1 if after, and 0
// if they are equal. Ms is the primary key and Seq breaks ties.
func (id StreamID) Compare(o StreamID) int {
	switch {
	case id.Ms < o.Ms:
		return -1
	case id.Ms > o.Ms:
		return 1
	case id.Seq < o.Seq:
		return -1
	case id.Seq > o.Seq:
		return 1
	default:
		return 0
	}
}

// streamSeqAuto is the sentinel Seq value ParseStreamID returns for the "ms-*"
// form, signalling that the sequence number should be auto-generated. It is the
// maximum uint64 and is also used as the implicit upper sequence bound for an
// incomplete end ID in a range query.
const streamSeqAuto = ^uint64(0)

// ParseStreamID parses a stream ID in one of three textual forms:
//
//	"ms"     -> StreamID{Ms: ms, Seq: 0}
//	"ms-seq" -> StreamID{Ms: ms, Seq: seq}
//	"ms-*"   -> StreamID{Ms: ms, Seq: streamSeqAuto} (auto-sequence sentinel)
//
// It returns an error if either component is not a valid base-10 unsigned
// integer (the "*" sequence being the sole exception).
func ParseStreamID(s string) (StreamID, error) {
	if s == "" {
		return StreamID{}, fmt.Errorf("ERR Invalid stream ID specified as stream command argument")
	}
	dash := strings.IndexByte(s, '-')
	if dash < 0 {
		ms, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return StreamID{}, fmt.Errorf("ERR Invalid stream ID specified as stream command argument")
		}
		return StreamID{Ms: ms}, nil
	}
	ms, err := strconv.ParseUint(s[:dash], 10, 64)
	if err != nil {
		return StreamID{}, fmt.Errorf("ERR Invalid stream ID specified as stream command argument")
	}
	seqPart := s[dash+1:]
	if seqPart == "*" {
		return StreamID{Ms: ms, Seq: streamSeqAuto}, nil
	}
	seq, err := strconv.ParseUint(seqPart, 10, 64)
	if err != nil {
		return StreamID{}, fmt.Errorf("ERR Invalid stream ID specified as stream command argument")
	}
	return StreamID{Ms: ms, Seq: seq}, nil
}

// StreamEntry is a single stream record: its ID plus a flat slice of
// alternating field,value,... pairs in insertion order.
type StreamEntry struct {
	// ID is the entry's stream ID.
	ID StreamID
	// Fields holds field,value,field,value,... in the order supplied to XAdd.
	Fields []string
}

// PendingEntry describes one entry in a consumer group's pending-entries list:
// the entry ID, the consumer currently owning it, how many times it has been
// delivered, and how long (in milliseconds, per the Store's Clock) since its
// last delivery.
type PendingEntry struct {
	// ID is the pending entry's stream ID.
	ID StreamID
	// Consumer is the name of the consumer that owns the entry.
	Consumer string
	// Deliveries is the number of times the entry has been delivered.
	Deliveries int64
	// IdleMs is the elapsed time since the entry was last delivered, in
	// milliseconds.
	IdleMs int64
}

// streamsAutoID computes the next fully auto-generated ID (the "*" form) for ms,
// guaranteeing the result is strictly greater than any previously assigned ID.
// The caller must hold streamReg.mu.
func streamsAutoID(st *streamState, ms uint64) StreamID {
	if streamsHasLast(st) {
		if ms < st.lastID.Ms {
			ms = st.lastID.Ms
		}
		if ms == st.lastID.Ms {
			return StreamID{Ms: ms, Seq: st.lastID.Seq + 1}
		}
	}
	return StreamID{Ms: ms, Seq: 0}
}

// streamsAutoSeq computes the ID for the "ms-*" form: the sequence continues
// within an existing ms, otherwise it starts at 0. A resulting ID that is not
// greater than the last one is rejected later by XAdd's monotonicity check. The
// caller must hold streamReg.mu.
func streamsAutoSeq(st *streamState, ms uint64) StreamID {
	if streamsHasLast(st) && ms == st.lastID.Ms {
		return StreamID{Ms: ms, Seq: st.lastID.Seq + 1}
	}
	return StreamID{Ms: ms, Seq: 0}
}

// XAdd appends an entry to the stream at key, creating the stream if needed, and
// returns the assigned ID. When id is "*" a monotonically increasing ID is
// generated using s.clock.Now() for the millisecond component and prev+1 for the
// sequence within the same millisecond; the "ms-*" form auto-generates only the
// sequence. An explicit ID must be strictly greater than the stream's current
// last ID. fields must contain an even number of elements (field,value,...) or
// ErrWrongArgs is returned.
func (s *Store) XAdd(key, id string, fields ...string) (StreamID, error) {
	if len(fields)%2 != 0 {
		return StreamID{}, ErrWrongArgs
	}
	streamReg.mu.Lock()
	defer streamReg.mu.Unlock()
	st := streamsStateFor(s, key, true)

	var newID StreamID
	switch {
	case id == "*":
		newID = streamsAutoID(st, uint64(s.clock.Now().UnixMilli()))
	default:
		if dash := strings.IndexByte(id, '-'); dash >= 0 && id[dash+1:] == "*" {
			ms, err := strconv.ParseUint(id[:dash], 10, 64)
			if err != nil {
				return StreamID{}, fmt.Errorf("ERR Invalid stream ID specified as stream command argument")
			}
			newID = streamsAutoSeq(st, ms)
		} else {
			pid, err := ParseStreamID(id)
			if err != nil {
				return StreamID{}, err
			}
			newID = pid
		}
	}

	if newID.Ms == 0 && newID.Seq == 0 {
		return StreamID{}, fmt.Errorf("ERR The ID specified in XADD must be greater than 0-0")
	}
	if streamsHasLast(st) && newID.Compare(st.lastID) <= 0 {
		return StreamID{}, fmt.Errorf("ERR The ID specified in XADD is equal or smaller than the target stream top item")
	}

	st.entries = append(st.entries, StreamEntry{ID: newID, Fields: append([]string(nil), fields...)})
	st.lastID = newID
	return newID, nil
}

// XLen returns the number of entries currently stored in the stream at key, or 0
// if no such stream exists.
func (s *Store) XLen(key string) int {
	streamReg.mu.Lock()
	defer streamReg.mu.Unlock()
	st := streamsStateFor(s, key, false)
	if st == nil {
		return 0
	}
	return len(st.entries)
}

// streamsParseBound parses a range endpoint. "-" and "+" denote the minimum and
// maximum possible IDs. An incomplete "ms" endpoint takes Seq 0 when it is the
// lower bound and the maximum Seq when it is the upper bound, so that a bare ms
// selects every entry sharing that millisecond.
func streamsParseBound(tok string, upper bool) (StreamID, error) {
	switch tok {
	case "-":
		return StreamID{}, nil
	case "+":
		return StreamID{Ms: streamSeqAuto, Seq: streamSeqAuto}, nil
	}
	if strings.IndexByte(tok, '-') < 0 {
		ms, err := strconv.ParseUint(tok, 10, 64)
		if err != nil {
			return StreamID{}, fmt.Errorf("ERR Invalid stream ID specified as stream command argument")
		}
		if upper {
			return StreamID{Ms: ms, Seq: streamSeqAuto}, nil
		}
		return StreamID{Ms: ms}, nil
	}
	return ParseStreamID(tok)
}

// streamsCollect returns the entries with IDs within [lo, hi] inclusive. When
// reverse is true the slice is ordered high to low. A count > 0 caps the number
// of results (applied after ordering). The caller must hold streamReg.mu.
func streamsCollect(st *streamState, lo, hi StreamID, count int, reverse bool) []StreamEntry {
	out := make([]StreamEntry, 0)
	if reverse {
		for i := len(st.entries) - 1; i >= 0; i-- {
			e := st.entries[i]
			if e.ID.Compare(lo) >= 0 && e.ID.Compare(hi) <= 0 {
				out = append(out, e)
				if count > 0 && len(out) >= count {
					break
				}
			}
		}
	} else {
		for _, e := range st.entries {
			if e.ID.Compare(lo) >= 0 && e.ID.Compare(hi) <= 0 {
				out = append(out, e)
				if count > 0 && len(out) >= count {
					break
				}
			}
		}
	}
	return out
}

// XRange returns entries with IDs in [start, end] in ascending order. "-" and
// "+" are open lower/upper bounds; an incomplete "ms" bound covers every
// sequence for that millisecond. A count <= 0 returns all matching entries.
func (s *Store) XRange(key, start, end string, count int) ([]StreamEntry, error) {
	lo, err := streamsParseBound(start, false)
	if err != nil {
		return nil, err
	}
	hi, err := streamsParseBound(end, true)
	if err != nil {
		return nil, err
	}
	streamReg.mu.Lock()
	defer streamReg.mu.Unlock()
	st := streamsStateFor(s, key, false)
	if st == nil {
		return []StreamEntry{}, nil
	}
	return streamsCollect(st, lo, hi, count, false), nil
}

// XRevRange returns entries with IDs in [start, end] in descending order. Note
// the reversed argument order (end before start) mirroring the Redis command. A
// count <= 0 returns all matching entries.
func (s *Store) XRevRange(key, end, start string, count int) ([]StreamEntry, error) {
	hi, err := streamsParseBound(end, true)
	if err != nil {
		return nil, err
	}
	lo, err := streamsParseBound(start, false)
	if err != nil {
		return nil, err
	}
	streamReg.mu.Lock()
	defer streamReg.mu.Unlock()
	st := streamsStateFor(s, key, false)
	if st == nil {
		return []StreamEntry{}, nil
	}
	return streamsCollect(st, lo, hi, count, true), nil
}

// XDel removes the entries with the given IDs from the stream at key and returns
// the number actually removed. A malformed ID yields an error; a missing stream
// yields 0. The stream's last ID is not reset by deletion.
func (s *Store) XDel(key string, ids ...string) (int, error) {
	parsed := make([]StreamID, len(ids))
	for i, raw := range ids {
		id, err := ParseStreamID(raw)
		if err != nil {
			return 0, err
		}
		parsed[i] = id
	}
	streamReg.mu.Lock()
	defer streamReg.mu.Unlock()
	st := streamsStateFor(s, key, false)
	if st == nil {
		return 0, nil
	}
	removed := 0
	for _, id := range parsed {
		for i, e := range st.entries {
			if e.ID.Compare(id) == 0 {
				st.entries = append(st.entries[:i], st.entries[i+1:]...)
				removed++
				break
			}
		}
	}
	return removed, nil
}

// streamsReadAfter parses an XREAD last-seen id for a stream, returning the ID
// after which entries should be reported. "$" means the stream's current last
// ID (so only entries added later would match). The caller must hold
// streamReg.mu.
func streamsReadAfter(st *streamState, id string) (StreamID, error) {
	if id == "$" {
		return st.lastID, nil
	}
	return ParseStreamID(id)
}

// XRead returns, for each stream in streams (mapping key to a last-seen ID), the
// entries whose IDs are strictly greater than that ID. Keys with no newer
// entries (or no stream at all) are omitted from the result. A count <= 0
// returns all newer entries per key.
func (s *Store) XRead(count int, streams map[string]string) (map[string][]StreamEntry, error) {
	streamReg.mu.Lock()
	defer streamReg.mu.Unlock()
	out := make(map[string][]StreamEntry)
	for key, rawID := range streams {
		st := streamsStateFor(s, key, false)
		if st == nil {
			continue
		}
		after, err := streamsReadAfter(st, rawID)
		if err != nil {
			return nil, err
		}
		got := make([]StreamEntry, 0)
		for _, e := range st.entries {
			if e.ID.Compare(after) > 0 {
				got = append(got, e)
				if count > 0 && len(got) >= count {
					break
				}
			}
		}
		if len(got) > 0 {
			out[key] = got
		}
	}
	return out, nil
}

// XGroupCreate creates the consumer group on the stream at key, starting
// delivery after the given ID. "$" starts after the current last ID (only new
// entries), "0" (or "0-0") from the beginning. When mkstream is true a missing
// stream is created; otherwise a missing stream is an error. Recreating an
// existing group is an error.
func (s *Store) XGroupCreate(key, group, id string, mkstream bool) error {
	streamReg.mu.Lock()
	defer streamReg.mu.Unlock()
	st := streamsStateFor(s, key, false)
	if st == nil {
		if !mkstream {
			return fmt.Errorf("ERR The XGROUP subcommand requires the key to exist. Note that for CREATE you may want to use the MKSTREAM option to create an empty stream automatically.")
		}
		st = streamsStateFor(s, key, true)
	}
	if _, ok := st.groups[group]; ok {
		return fmt.Errorf("BUSYGROUP Consumer Group name already exists")
	}
	var start StreamID
	if id == "$" {
		start = st.lastID
	} else {
		pid, err := ParseStreamID(id)
		if err != nil {
			return err
		}
		start = pid
	}
	st.groups[group] = &streamGroup{lastDelivered: start, pending: map[StreamID]*streamPending{}}
	return nil
}

// XGroupDestroy removes the named consumer group from the stream at key,
// returning whether a group was actually removed.
func (s *Store) XGroupDestroy(key, group string) (bool, error) {
	streamReg.mu.Lock()
	defer streamReg.mu.Unlock()
	st := streamsStateFor(s, key, false)
	if st == nil {
		return false, nil
	}
	if _, ok := st.groups[group]; !ok {
		return false, nil
	}
	delete(st.groups, group)
	return true, nil
}

// XReadGroup reads entries on behalf of consumer within group across the given
// streams. For a key mapped to ">", new entries (those after the group's
// last-delivered ID) are delivered, the group's cursor advances, and each
// delivered entry is recorded in the pending-entries list under consumer. For a
// key mapped to a real ID, the consumer's own pending entries with IDs strictly
// greater than that ID are re-read (pass "0" to re-read all of them). Keys that
// produce no entries are omitted. A count <= 0 returns all eligible entries.
func (s *Store) XReadGroup(group, consumer string, count int, streams map[string]string) (map[string][]StreamEntry, error) {
	streamReg.mu.Lock()
	defer streamReg.mu.Unlock()
	nowMs := s.clock.Now().UnixMilli()
	out := make(map[string][]StreamEntry)
	for key, rawID := range streams {
		st := streamsStateFor(s, key, false)
		if st == nil {
			return nil, fmt.Errorf("NOGROUP No such key '%s' or consumer group '%s'", key, group)
		}
		g, ok := st.groups[group]
		if !ok {
			return nil, fmt.Errorf("NOGROUP No such key '%s' or consumer group '%s'", key, group)
		}
		got := make([]StreamEntry, 0)
		if rawID == ">" {
			for _, e := range st.entries {
				if e.ID.Compare(g.lastDelivered) > 0 {
					got = append(got, e)
					g.lastDelivered = e.ID
					g.pending[e.ID] = &streamPending{consumer: consumer, deliveries: 1, deliveredMs: nowMs}
					if count > 0 && len(got) >= count {
						break
					}
				}
			}
		} else {
			after, err := ParseStreamID(rawID)
			if err != nil {
				return nil, err
			}
			ids := make([]StreamID, 0, len(g.pending))
			for id, p := range g.pending {
				if p.consumer == consumer && id.Compare(after) > 0 {
					ids = append(ids, id)
				}
			}
			sort.Slice(ids, func(i, j int) bool { return ids[i].Compare(ids[j]) < 0 })
			for _, id := range ids {
				if e, found := streamsFindEntry(st, id); found {
					got = append(got, e)
				} else {
					got = append(got, StreamEntry{ID: id})
				}
				g.pending[id].deliveries++
				g.pending[id].deliveredMs = nowMs
				if count > 0 && len(got) >= count {
					break
				}
			}
		}
		if len(got) > 0 {
			out[key] = got
		}
	}
	return out, nil
}

// XAck removes the given IDs from the pending-entries list of group on the
// stream at key, returning the number of entries actually acknowledged. Unknown
// IDs, a missing group, or a missing stream contribute nothing.
func (s *Store) XAck(key, group string, ids ...string) (int, error) {
	parsed := make([]StreamID, len(ids))
	for i, raw := range ids {
		id, err := ParseStreamID(raw)
		if err != nil {
			return 0, err
		}
		parsed[i] = id
	}
	streamReg.mu.Lock()
	defer streamReg.mu.Unlock()
	st := streamsStateFor(s, key, false)
	if st == nil {
		return 0, nil
	}
	g, ok := st.groups[group]
	if !ok {
		return 0, nil
	}
	acked := 0
	for _, id := range parsed {
		if _, ok := g.pending[id]; ok {
			delete(g.pending, id)
			acked++
		}
	}
	return acked, nil
}

// streamsPendingList materializes group g's pending-entries list as a sorted
// slice of PendingEntry, computing idle times against nowMs. The caller must
// hold streamReg.mu.
func streamsPendingList(g *streamGroup, nowMs int64) []PendingEntry {
	out := make([]PendingEntry, 0, len(g.pending))
	for id, p := range g.pending {
		out = append(out, PendingEntry{
			ID:         id,
			Consumer:   p.consumer,
			Deliveries: p.deliveries,
			IdleMs:     nowMs - p.deliveredMs,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID.Compare(out[j].ID) < 0 })
	return out
}

// XPending summarizes the pending-entries list of group on the stream at key: the
// total count, the smallest and largest pending IDs (zero values when empty),
// and a per-consumer count of owned entries. A missing stream or group is an
// error.
func (s *Store) XPending(key, group string) (count int, min, max StreamID, perConsumer map[string]int64, err error) {
	streamReg.mu.Lock()
	defer streamReg.mu.Unlock()
	st := streamsStateFor(s, key, false)
	if st == nil {
		return 0, StreamID{}, StreamID{}, nil, fmt.Errorf("NOGROUP No such key '%s' or consumer group '%s'", key, group)
	}
	g, ok := st.groups[group]
	if !ok {
		return 0, StreamID{}, StreamID{}, nil, fmt.Errorf("NOGROUP No such key '%s' or consumer group '%s'", key, group)
	}
	list := streamsPendingList(g, s.clock.Now().UnixMilli())
	perConsumer = make(map[string]int64)
	for _, pe := range list {
		perConsumer[pe.Consumer]++
	}
	if len(list) == 0 {
		return 0, StreamID{}, StreamID{}, perConsumer, nil
	}
	return len(list), list[0].ID, list[len(list)-1].ID, perConsumer, nil
}

// streamsEntryReply converts a StreamEntry into the RESP-style nested array
// reply [idString, [field, value, ...]].
func streamsEntryReply(e StreamEntry) []any {
	fields := make([]any, len(e.Fields))
	for i, f := range e.Fields {
		fields[i] = f
	}
	return []any{e.ID.String(), fields}
}

// streamsEntriesReply converts a slice of entries into an array reply.
func streamsEntriesReply(entries []StreamEntry) []any {
	out := make([]any, len(entries))
	for i, e := range entries {
		out[i] = streamsEntryReply(e)
	}
	return out
}

// streamsCmdXAdd implements the XADD command dispatch entry.
func streamsCmdXAdd(s *Store, a []string) (any, error) {
	if len(a) < 4 {
		return nil, ErrWrongArgs
	}
	id, err := s.XAdd(a[0], a[1], a[2:]...)
	if err != nil {
		return nil, err
	}
	return id.String(), nil
}

// streamsCmdXLen implements the XLEN command dispatch entry.
func streamsCmdXLen(s *Store, a []string) (any, error) {
	if len(a) != 1 {
		return nil, ErrWrongArgs
	}
	return int64(s.XLen(a[0])), nil
}

// streamsCmdXRange implements the XRANGE command dispatch entry, accepting an
// optional trailing "COUNT n".
func streamsCmdXRange(s *Store, a []string) (any, error) {
	count := 0
	switch {
	case len(a) == 3:
	case len(a) == 5 && strings.EqualFold(a[3], "COUNT"):
		n, err := strconv.Atoi(a[4])
		if err != nil {
			return nil, ErrNotInteger
		}
		count = n
	default:
		return nil, ErrSyntax
	}
	entries, err := s.XRange(a[0], a[1], a[2], count)
	if err != nil {
		return nil, err
	}
	return streamsEntriesReply(entries), nil
}

// streamsCmdXRead implements the XREAD command dispatch entry, supporting an
// optional leading "COUNT n" followed by "STREAMS key... id...". The reply
// preserves the order of keys as given.
func streamsCmdXRead(s *Store, a []string) (any, error) {
	i := 0
	count := 0
	if len(a) >= 2 && strings.EqualFold(a[0], "COUNT") {
		n, err := strconv.Atoi(a[1])
		if err != nil {
			return nil, ErrNotInteger
		}
		count = n
		i = 2
	}
	if i >= len(a) || !strings.EqualFold(a[i], "STREAMS") {
		return nil, ErrSyntax
	}
	rest := a[i+1:]
	if len(rest) == 0 || len(rest)%2 != 0 {
		return nil, ErrSyntax
	}
	n := len(rest) / 2
	keys := rest[:n]
	streams := make(map[string]string, n)
	for j := 0; j < n; j++ {
		streams[keys[j]] = rest[n+j]
	}
	res, err := s.XRead(count, streams)
	if err != nil {
		return nil, err
	}
	out := make([]any, 0, len(res))
	for _, k := range keys {
		entries, ok := res[k]
		if !ok {
			continue
		}
		out = append(out, []any{k, streamsEntriesReply(entries)})
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// init registers the read-oriented stream commands into the package dispatch
// table. This file sorts after dispatch.go, so dispatch.go's init has already
// built the table by the time this runs; the nil guard keeps the registration
// robust regardless of init ordering.
func init() {
	if dispatchTable == nil {
		dispatchTable = map[string]handler{}
	}
	dispatchTable["XADD"] = streamsCmdXAdd
	dispatchTable["XLEN"] = streamsCmdXLen
	dispatchTable["XRANGE"] = streamsCmdXRange
	dispatchTable["XREAD"] = streamsCmdXRead
}
