// Library content for the redis documentation site. Mirrors the shape used by
// the malcolmston/go landing site's data.ts so the sibling sites stay in sync.
export interface Lib {
  id: string; name: string; icon: string; accent: string; pkg: string; node: string;
  repo: string; docs: string; tagline: string; blurb: string; tags: string[];
  features: string[]; node_code: string; go_code: string; integrate: string;
}

export const NODE_ACCENT = '#8cc84b';

export const REDIS: Lib = {
  id:"redis", name:"Redis", icon:'<i class="fa-solid fa-database"></i>', accent:"#dc382d",
  pkg:"github.com/malcolmston/redis", node:"redis/redis",
  repo:"https://github.com/malcolmston/redis", docs:"https://malcolmston.github.io/redis/",
  tagline:"An embeddable, Redis-style in-memory data store in pure Go.",
  blurb:"A thread-safe, Redis-style keyspace built entirely on the Go standard library — no cgo and no third-party "+
    "dependencies. A single-mutex Store holds the core Redis data types (strings, lists, hashes, sets and "+
    "skiplist-backed sorted sets), exposed both as typed Go methods and through a dynamic Do(...) dispatcher that "+
    "mirrors sending a RESP command array. Expiration is lazy and driven by an injectable Clock so TTL behaviour is "+
    "fully deterministic in tests, and a RESP2 codec plus an optional TCP Server let real Redis clients speak to the "+
    "same store over the wire.",
  tags:["Store","strings","lists","hashes","sets","sorted sets","skiplist","lazy expiry","RESP2 codec","Do dispatcher","TCP server","stdlib-only"],
  features:[
    "<code>Store</code> — a thread-safe, single-mutex keyspace built with <code>New</code> or <code>NewWithClock</code>, plus generic <code>Del</code>, <code>Exists</code>, <code>Keys</code> (glob), <code>TypeOf</code>, <code>DBSize</code> and <code>FlushAll</code>",
    "Strings — <code>Set</code> (with EX/PX/NX/XX <code>SetOptions</code>), <code>Get</code>, <code>GetSet</code>, <code>Append</code>, <code>Strlen</code> and <code>Incr</code>/<code>Decr</code>/<code>IncrBy</code>/<code>DecrBy</code>",
    "Lists — <code>LPush</code>, <code>RPush</code>, <code>LPop</code>, <code>RPop</code>, <code>LRange</code>, <code>LLen</code> and <code>LIndex</code>",
    "Hashes — <code>HSet</code>, <code>HGet</code>, <code>HDel</code>, <code>HGetAll</code>, <code>HKeys</code>, <code>HVals</code>, <code>HLen</code> and <code>HExists</code>",
    "Sets — <code>SAdd</code>, <code>SRem</code>, <code>SMembers</code>, <code>SIsMember</code>, <code>SCard</code> plus <code>SInter</code>, <code>SUnion</code> and <code>SDiff</code>",
    "Sorted sets — <code>ZAdd</code>, <code>ZScore</code>, <code>ZRange</code>, <code>ZRevRange</code>, <code>ZRangeByScore</code>, <code>ZRank</code> and <code>ZRevRank</code>, backed by a <code>skiplist</code> ordered by (score, member) for O(log n) rank queries",
    "Lazy, deterministic expiry — <code>Expire</code>/<code>PExpire</code>/<code>TTL</code>/<code>PTTL</code>/<code>Persist</code> with an injectable <code>Clock</code> and a testable <code>ManualClock</code>",
    "Dynamic dispatch — <code>Store.Do</code> runs a command by name and returns RESP-friendly Go values (<code>SimpleString</code>, <code>int64</code>, <code>string</code>, <code>nil</code> or <code>[]any</code>)",
    "RESP2 codec — <code>Encoder</code> and <code>Decoder</code> (<code>NewEncoder</code>/<code>NewDecoder</code>) implement the RESP wire format, and an optional <code>Server</code> (<code>NewServer</code>/<code>ListenAndServe</code>) speaks it to real Redis clients over TCP",
    "Zero dependencies — pure Go standard library, no cgo, nothing to audit but the toolchain"
  ],
  node_code:
`# redis-cli speaking RESP to a real Redis server
SET name alice
GET name             # "alice"

RPUSH tasks a b c
LRANGE tasks 0 -1    # 1) "a" 2) "b" 3) "c"

ZADD board 25 bob
ZREVRANK board bob   # (integer) 0`,
  go_code:
`import "github.com/malcolmston/redis"

s := redis.New()

s.Set("name", "alice", redis.SetOptions{})
name, _, _ := s.Get("name")           // "alice"

s.RPush("tasks", "a", "b", "c")
items, _ := s.LRange("tasks", 0, -1)  // [a b c]

s.ZAdd("board", redis.ZMember{Member: "bob", Score: 25})
rank, _, _ := s.ZRevRank("board", "bob") // 0`,
  integrate:
`<span class="tok-c">// Every command is also a dynamic Do(...) call that mirrors sending a</span>
<span class="tok-c">// RESP array and returns RESP-friendly Go values.</span>
s := redis.New()
s.Do("SET", "n", "1")
n, _ := s.Do("INCR", "n")        <span class="tok-c">// int64(2)</span>
t, _ := s.Do("TYPE", "n")        <span class="tok-c">// redis.SimpleString("string")</span>

<span class="tok-c">// Inject a ManualClock so lazy, on-access expiry is deterministic.</span>
clk := redis.NewManualClock(time.Unix(0, 0))
s = redis.NewWithClock(clk)
s.Set("k", "v", redis.SetOptions{EX: 10 * time.Second})
clk.Advance(11 * time.Second)
_, ok, _ := s.Get("k")           <span class="tok-c">// ok == false: expired on access</span>

<span class="tok-c">// Serve RESP2 over TCP so a real redis-cli can connect to the store.</span>
srv := redis.NewServer(s)
go srv.ListenAndServe(":6379")
defer srv.Close()`
};
