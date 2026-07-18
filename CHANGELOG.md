# Changelog

All notable changes to this project are documented here. This project adheres to
semantic versioning.

## [0.3.0]

Added 63 new exported functions and types, moving the library closer to parity
with Redis. Everything is implemented with the Go standard library only, with
deterministic behavior and known-answer tests for each new file.

### Strings (`stringsext.go`)
- `GetDel`, `GetEx` (+ `GetExOptions`) — GETDEL / GETEX with TTL control.
- `SubStr` — SUBSTR alias of GETRANGE.
- `Lcs`, `LcsLen`, `LcsIdx` (+ `LcsMatch`) — longest common subsequence,
  including the IDX/WITHMATCHLEN match runs.

### Bitmaps (`bitfieldext.go`)
- `BitFieldGet`, `BitFieldSet`, `BitFieldIncrBy` (+ `BitFieldType`,
  `BitFieldOverflow`) — the BITFIELD sub-operations with WRAP/SAT/FAIL overflow.
- `BitCountRange` — BITCOUNT with byte and BIT range modes.

### Lists (`listsext.go`)
- `LPushX`, `RPushX`, `LPopN`, `RPopN`, `LMPop` (+ `ListDirection`).

### Sets (`setsext.go`)
- `SMIsMember`, `SInterCard`.

### Sorted sets (`zsetext.go`, `zaddext.go`, `zrangeext.go`)
- `ZLexCount`, `ZRemRangeByLex`, `ZDiff`, `ZDiffStore`, `ZUnion`, `ZInter`,
  `ZInterCard`, `ZRandMember`, `ZRangeStore`, `ZMPopMin`, `ZMPopMax`.
- `ZAddWith` (+ `ZAddOptions`) — ZADD with NX/XX/GT/LT/CH.
- `ZRevRangeByScore`, `ZRangeByScoreLimit`, `ZRevRangeByScoreLimit`,
  `ZRevRangeByLex`, `ZRangeByLexLimit`, `ZRandMemberWithScores`.

### Hashes (`hashext.go`)
- `HGetDel`, `HRandFieldWithValues` (+ `HashEntry`).

### Generic keyspace (`genericext.go`)
- `ExpireAt`, `PExpireAt`, `ExpireTime`, `PExpireTime`.
- `ExpireWith` (+ `ExpireCond`) — EXPIRE with NX/XX/GT/LT.
- `Sort`, `SortStore` (+ `SortOptions`) — SORT ordering and LIMIT (BY/GET
  patterns are not supported).

### Introspection (`objectext.go`)
- `ObjectEncoding`, `ObjectRefCount`, `ObjectIdleTime`, `ObjectFreq`,
  `MemoryUsage`.

### Persistence (`snapshot.go`)
- `MarshalSnapshot`, `LoadSnapshot`, `NewFromSnapshot`, `DumpKey`, `RestoreKey`
  (+ `ErrBadSnapshot`, `ErrBusyKey`) — a deterministic RDB-like snapshot format
  for the five core data types plus per-key DUMP/RESTORE. Stream keys, which
  live outside the ordinary keyspace, are not yet included.

## [0.2.0]

Initial documented release: strings, expiration, lists, hashes, sets, sorted
sets, streams, geo, HyperLogLog, bitmaps, pub/sub, transactions, scanning, the
RESP codec, the command dispatcher, and the optional TCP server.
