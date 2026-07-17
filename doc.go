// Package redis is an embeddable, in-memory, Redis-style data-structure store
// written entirely with the Go standard library.
//
// It provides a thread-safe keyspace holding typed values, a RESP (REdis
// Serialization Protocol) codec, an in-process command dispatcher, and an
// optional TCP server. It has no external dependencies and no cgo.
//
// # Store
//
// The central type is Store, a thread-safe keyspace guarded by a single mutex.
// Construct one with New for production use, or NewWithClock to inject a Clock
// (typically a *ManualClock) so that expiration behaves deterministically in
// tests:
//
//	s := redis.New()
//	s.Set("greeting", "hello", redis.SetOptions{})
//	v, ok, _ := s.Get("greeting") // "hello", true
//
// Every command is available both as a typed Go method (for example Get,
// LPush, ZAdd) and through the dynamic dispatcher Do.
//
// # Data types
//
// The store supports the core Redis data types:
//
//   - Strings: SET (with EX/PX/NX/XX), GET, GETSET, APPEND, STRLEN,
//     INCR/DECR/INCRBY/DECRBY, plus DEL/EXISTS.
//   - Expiration: EXPIRE, PEXPIRE, TTL, PTTL, PERSIST. Expiry is lazy: expired
//     keys are removed on access and skipped by scans. The clock is injectable.
//   - Lists: LPUSH, RPUSH, LPOP, RPOP, LRANGE, LLEN, LINDEX.
//   - Hashes: HSET, HGET, HDEL, HGETALL, HKEYS, HVALS, HLEN, HEXISTS.
//   - Sets: SADD, SREM, SMEMBERS, SISMEMBER, SCARD, SINTER, SUNION, SDIFF.
//   - Sorted sets: ZADD, ZREM, ZSCORE, ZRANGE, ZREVRANGE, ZRANGEBYSCORE,
//     ZRANK, ZREVRANK, ZCARD. These are backed by a skiplist ordered by
//     (score, member) with lexicographic tie-breaking, giving O(log n)
//     insertion, deletion, and rank queries.
//   - Generic: KEYS (glob patterns), TYPE, FLUSHALL, DBSIZE.
//
// Applying a command to a key of the wrong type returns ErrWrongType, matching
// Redis's WRONGTYPE behavior.
//
// # RESP codec
//
// Encoder and Decoder implement the RESP2 wire format for the five value
// types: simple strings (+), errors (-), integers (:), bulk strings ($, and
// the null bulk string), and arrays (*, and the null array). The helper types
// SimpleString and RESPError distinguish status and error replies from ordinary
// bulk strings.
//
//	var buf bytes.Buffer
//	enc := redis.NewEncoder(&buf)
//	enc.Encode([]any{"foo", int64(42)})
//	dec := redis.NewDecoder(&buf)
//	v, _ := dec.Decode() // []any{"foo", int64(42)}
//
// # Command dispatch
//
// Store.Do executes a command by name and string arguments, returning a
// RESP-friendly Go value (SimpleString, int64, string, nil, or []any). It is
// the in-process equivalent of a client sending a RESP command array:
//
//	s.Do("SET", "n", "1")
//	s.Do("INCR", "n")      // int64(2)
//	s.Do("TYPE", "n")      // SimpleString("string")
//
// # TCP server
//
// Server wraps a Store and speaks RESP over TCP so real Redis clients can
// connect. It is optional and independent of the core store, so the
// deterministic tests never rely on it:
//
//	srv := redis.NewServer(s)
//	go srv.ListenAndServe(":6379")
//	defer srv.Close()
package redis
