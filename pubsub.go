package redis

import (
	"sort"
	"sync"
)

// pubsubBufferSize is the capacity of every Subscription's delivery channel.
// Deliveries beyond this backlog are dropped rather than blocking a publisher.
const pubsubBufferSize = 16

// pubsubHub holds the channel and pattern subscription tables for a single
// Store. It is guarded by its own RWMutex so that publishes (readers) proceed
// concurrently while subscribes and closes (writers) mutate the tables.
type pubsubHub struct {
	mu       sync.RWMutex
	channels map[string]map[*Subscription]struct{}
	patterns map[string]map[*Subscription]struct{}
}

// pubsubReg is the package-level registry mapping each Store to its pubsubHub.
// Pub/sub state cannot live on the Store struct, so it is kept here keyed by the
// Store pointer and guarded by its own mutex.
var pubsubReg = struct {
	mu sync.Mutex
	m  map[*Store]*pubsubHub
}{m: map[*Store]*pubsubHub{}}

// pubsubHubFor returns the pubsubHub for s, creating and registering one on
// first use. It is safe for concurrent callers.
func pubsubHubFor(s *Store) *pubsubHub {
	pubsubReg.mu.Lock()
	defer pubsubReg.mu.Unlock()
	h := pubsubReg.m[s]
	if h == nil {
		h = &pubsubHub{
			channels: map[string]map[*Subscription]struct{}{},
			patterns: map[string]map[*Subscription]struct{}{},
		}
		pubsubReg.m[s] = h
	}
	return h
}

// pubsubDedup returns in with duplicate entries removed, preserving first-seen
// order.
func pubsubDedup(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// Message is a single pub/sub delivery. Pattern is empty for messages delivered
// to an exact-channel subscription and holds the matching pattern for messages
// delivered to a pattern subscription.
type Message struct {
	// Channel is the channel the message was published to.
	Channel string
	// Pattern is the subscription pattern that matched, or empty for an
	// exact-channel delivery.
	Pattern string
	// Payload is the published message body.
	Payload string
}

// Subscription is an active pub/sub subscription obtained from Store.Subscribe
// or Store.PSubscribe. Messages are delivered on a buffered channel; a caller
// must drain Messages promptly, as deliveries are dropped when the buffer is
// full. A Subscription must be closed with Close when no longer needed.
type Subscription struct {
	ch       chan Message
	hub      *pubsubHub
	channels []string
	patterns []string
	closed   bool
}

// pubsubDeliver performs a non-blocking send of m to the subscription's buffer,
// silently dropping the message if the buffer is full.
func (sub *Subscription) pubsubDeliver(m Message) {
	select {
	case sub.ch <- m:
	default:
	}
}

// Messages returns the receive-only stream of deliveries for this subscription.
// The channel is closed by Close.
func (sub *Subscription) Messages() <-chan Message { return sub.ch }

// Channels returns the exact channels this subscription is subscribed to, sorted
// lexicographically. For a pattern subscription the result is empty.
func (sub *Subscription) Channels() []string {
	out := append([]string(nil), sub.channels...)
	sort.Strings(out)
	return out
}

// Close unregisters the subscription from its Store and closes the delivery
// channel returned by Messages. It is idempotent; calling it more than once has
// no additional effect.
func (sub *Subscription) Close() {
	sub.hub.mu.Lock()
	defer sub.hub.mu.Unlock()
	if sub.closed {
		return
	}
	sub.closed = true
	for _, c := range sub.channels {
		if set := sub.hub.channels[c]; set != nil {
			delete(set, sub)
			if len(set) == 0 {
				delete(sub.hub.channels, c)
			}
		}
	}
	for _, p := range sub.patterns {
		if set := sub.hub.patterns[p]; set != nil {
			delete(set, sub)
			if len(set) == 0 {
				delete(sub.hub.patterns, p)
			}
		}
	}
	close(sub.ch)
}

// Subscribe returns a Subscription that receives messages published to any of
// the given exact channels. Duplicate channels are ignored. The returned
// Subscription must be closed with Close when no longer needed.
func (s *Store) Subscribe(channels ...string) *Subscription {
	h := pubsubHubFor(s)
	sub := &Subscription{
		ch:       make(chan Message, pubsubBufferSize),
		hub:      h,
		channels: pubsubDedup(channels),
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range sub.channels {
		set := h.channels[c]
		if set == nil {
			set = map[*Subscription]struct{}{}
			h.channels[c] = set
		}
		set[sub] = struct{}{}
	}
	return sub
}

// PSubscribe returns a Subscription that receives messages published to any
// channel matching one of the given glob-style patterns (see Match). Duplicate
// patterns are ignored. The returned Subscription must be closed with Close when
// no longer needed.
func (s *Store) PSubscribe(patterns ...string) *Subscription {
	h := pubsubHubFor(s)
	sub := &Subscription{
		ch:       make(chan Message, pubsubBufferSize),
		hub:      h,
		patterns: pubsubDedup(patterns),
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range sub.patterns {
		set := h.patterns[p]
		if set == nil {
			set = map[*Subscription]struct{}{}
			h.patterns[p] = set
		}
		set[sub] = struct{}{}
	}
	return sub
}

// Publish sends payload to channel and returns the number of receivers: the
// count of exact subscribers to channel plus the count of pattern subscriptions
// whose pattern matches channel. Delivery is non-blocking, so a receiver whose
// buffer is full is still counted but does not receive the message.
func (s *Store) Publish(channel, payload string) int {
	h := pubsubHubFor(s)
	h.mu.RLock()
	defer h.mu.RUnlock()
	count := 0
	for sub := range h.channels[channel] {
		sub.pubsubDeliver(Message{Channel: channel, Payload: payload})
		count++
	}
	for pat, subs := range h.patterns {
		if !Match(pat, channel) {
			continue
		}
		for sub := range subs {
			sub.pubsubDeliver(Message{Channel: channel, Pattern: pat, Payload: payload})
			count++
		}
	}
	return count
}

// PubSubChannels returns the active exact channels (those with at least one
// subscriber), sorted lexicographically. An empty pattern or "*" lists every
// active channel; any other pattern filters the result via Match.
func (s *Store) PubSubChannels(pattern string) []string {
	h := pubsubHubFor(s)
	h.mu.RLock()
	defer h.mu.RUnlock()
	all := pattern == "" || pattern == "*"
	out := make([]string, 0, len(h.channels))
	for c, subs := range h.channels {
		if len(subs) == 0 {
			continue
		}
		if all || Match(pattern, c) {
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

// PubSubNumSub returns a map from each of the given channels to its number of
// exact subscribers. Channels with no subscribers map to zero.
func (s *Store) PubSubNumSub(channels ...string) map[string]int {
	h := pubsubHubFor(s)
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[string]int, len(channels))
	for _, c := range channels {
		out[c] = len(h.channels[c])
	}
	return out
}

// PubSubNumPat returns the number of distinct patterns that currently have at
// least one pattern subscription.
func (s *Store) PubSubNumPat() int {
	h := pubsubHubFor(s)
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.patterns)
}
