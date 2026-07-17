# redis

Embeddable in-memory data structure store for Go.

`github.com/malcolmston/redis` is a thread-safe, Redis-style keyspace built
entirely on the Go standard library. It has no third-party dependencies and no
cgo. It offers the core Redis data types and commands, a RESP (REdis
Serialization Protocol) codec, an in-process command dispatcher, and an optional
TCP server that speaks RESP to real Redis clients.

## Install

```sh
go get github.com/malcolmston/redis
```

Requires Go 1.24 or newer.

## Quick start

Use the typed Go API:

```go
package main

import (
	"fmt"

	"github.com/malcolmston/redis"
)

func main() {
	s := redis.New()

	s.Set("name", "alice", redis.SetOptions{})
	name, _, _ := s.Get("name")
	fmt.Println(name) // alice

	s.RPush("tasks", "a", "b", "c")
	items, _ := s.LRange("tasks", 0, -1)
	fmt.Println(items) // [a b c]

	s.ZAdd("board", redis.ZMember{Member: "bob", Score: 25})
	rank, _, _ := s.ZRevRank("board", "bob")
	fmt.Println(rank) // 0
}
```

Or the dynamic dispatcher, `Do`, which mirrors sending a RESP command array:

```go
s := redis.New()
s.Do("SET", "n", "1")
n, _ := s.Do("INCR", "n") // int64(2)
t, _ := s.Do("TYPE", "n") // redis.SimpleString("string")
```

`Do` returns RESP-friendly Go values: `SimpleString`, `int64`, `string`, `nil`
(null reply), or `[]any` (array reply).

## Commands

| Group        | Commands |
|--------------|----------|
| Strings      | `SET` (EX/PX/NX/XX), `GET`, `GETSET`, `APPEND`, `STRLEN`, `INCR`, `DECR`, `INCRBY`, `DECRBY` |
| Expiration   | `EXPIRE`, `PEXPIRE`, `TTL`, `PTTL`, `PERSIST` (lazy, on-access expiry) |
| Lists        | `LPUSH`, `RPUSH`, `LPOP`, `RPOP`, `LRANGE`, `LLEN`, `LINDEX` |
| Hashes       | `HSET`, `HGET`, `HDEL`, `HGETALL`, `HKEYS`, `HVALS`, `HLEN`, `HEXISTS` |
| Sets         | `SADD`, `SREM`, `SMEMBERS`, `SISMEMBER`, `SCARD`, `SINTER`, `SUNION`, `SDIFF` |
| Sorted sets  | `ZADD`, `ZREM`, `ZSCORE`, `ZRANGE`, `ZREVRANGE`, `ZRANGEBYSCORE`, `ZRANK`, `ZREVRANK`, `ZCARD` |
| Generic      | `DEL`, `EXISTS`, `KEYS` (glob), `TYPE`, `FLUSHALL`, `DBSIZE` |

Sorted sets are backed by a skiplist ordered by `(score, member)` with
lexicographic tie-breaking, giving O(log n) insertion, deletion, and rank
queries.

## Deterministic expiration

Inject a clock to make TTL behavior fully deterministic in tests:

```go
clk := redis.NewManualClock(time.Unix(0, 0))
s := redis.NewWithClock(clk)

s.Set("k", "v", redis.SetOptions{EX: 10 * time.Second})
clk.Advance(11 * time.Second)

_, ok, _ := s.Get("k") // ok == false: expired on access
```

## RESP codec

`Encoder` and `Decoder` implement the RESP2 wire format for simple strings,
errors, integers, bulk strings, and arrays (including the null bulk string and
null array):

```go
var buf bytes.Buffer
redis.NewEncoder(&buf).Encode([]any{"foo", int64(42)})
v, _ := redis.NewDecoder(&buf).Decode() // []any{"foo", int64(42)}
```

## TCP server

`Server` wraps a `Store` and serves RESP over TCP so standard Redis clients can
connect:

```go
s := redis.New()
srv := redis.NewServer(s)
go srv.ListenAndServe(":6379")
defer srv.Close()
```

The server is independent of the core store and off the deterministic test path.

## Development

```sh
go build ./...
go vet ./...
go test ./...
```

## License

See repository.
