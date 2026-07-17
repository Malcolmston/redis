package redis_test

import (
	"fmt"

	"github.com/malcolmston/redis"
)

// Example demonstrates the in-process command dispatcher across several data
// types.
func Example() {
	s := redis.New()

	// Strings and counters.
	_, _ = s.Do("SET", "user:1:name", "alice")
	_, _ = s.Do("SET", "user:1:visits", "0")
	_, _ = s.Do("INCR", "user:1:visits")
	_, _ = s.Do("INCRBY", "user:1:visits", "4")

	name, _ := s.Do("GET", "user:1:name")
	visits, _ := s.Do("GET", "user:1:visits")
	fmt.Printf("%s has %s visits\n", name, visits)

	// A sorted set as a leaderboard.
	_, _ = s.Do("ZADD", "board", "10", "alice", "25", "bob", "5", "carol")
	top, _ := s.Do("ZREVRANGE", "board", "0", "-1", "WITHSCORES")
	fmt.Println("leaderboard:", top)

	rank, _ := s.Do("ZREVRANK", "board", "alice")
	fmt.Println("alice rank:", rank)

	// Output:
	// alice has 5 visits
	// leaderboard: [bob 25 alice 10 carol 5]
	// alice rank: 1
}

// ExampleStore_Get shows the typed method API with a hit and a miss.
func ExampleStore_Get() {
	s := redis.New()
	s.Set("greeting", "hello", redis.SetOptions{})

	v, ok, _ := s.Get("greeting")
	fmt.Println(v, ok)

	_, ok, _ = s.Get("absent")
	fmt.Println(ok)

	// Output:
	// hello true
	// false
}
