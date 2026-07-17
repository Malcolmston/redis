package redis

import (
	"reflect"
	"sort"
	"testing"
)

// drain reads all currently-buffered messages from a subscription without
// blocking and returns them in arrival order.
func drainMessages(sub *Subscription) []Message {
	var out []Message
	for {
		select {
		case m := <-sub.Messages():
			out = append(out, m)
		default:
			return out
		}
	}
}

func TestPublishExactChannels(t *testing.T) {
	tests := []struct {
		name      string
		subscribe []string
		publishOn string
		payload   string
		wantCount int
		wantMsg   bool
	}{
		{"single match", []string{"news"}, "news", "hello", 1, true},
		{"no match", []string{"news"}, "sports", "hello", 0, false},
		{"duplicate channels deduped", []string{"news", "news"}, "news", "x", 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			sub := s.Subscribe(tt.subscribe...)
			defer sub.Close()

			if got := s.Publish(tt.publishOn, tt.payload); got != tt.wantCount {
				t.Fatalf("Publish count = %d, want %d", got, tt.wantCount)
			}
			msgs := drainMessages(sub)
			if tt.wantMsg {
				if len(msgs) != 1 {
					t.Fatalf("got %d messages, want 1", len(msgs))
				}
				want := Message{Channel: tt.publishOn, Payload: tt.payload}
				if msgs[0] != want {
					t.Fatalf("message = %+v, want %+v", msgs[0], want)
				}
			} else if len(msgs) != 0 {
				t.Fatalf("got %d messages, want 0", len(msgs))
			}
		})
	}
}

func TestPublishMultipleSubscribers(t *testing.T) {
	s := New()
	a := s.Subscribe("c")
	b := s.Subscribe("c")
	defer a.Close()
	defer b.Close()

	if got := s.Publish("c", "v"); got != 2 {
		t.Fatalf("Publish count = %d, want 2", got)
	}
	for _, sub := range []*Subscription{a, b} {
		msgs := drainMessages(sub)
		if len(msgs) != 1 || msgs[0].Payload != "v" {
			t.Fatalf("subscriber got %+v, want one payload v", msgs)
		}
	}
}

func TestPSubscribePatternDelivery(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		channel   string
		wantCount int
	}{
		{"star suffix match", "news.*", "news.tech", 1},
		{"star matches all", "*", "anything", 1},
		{"question mark", "ab?", "abc", 1},
		{"no match", "news.*", "sports.x", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			sub := s.PSubscribe(tt.pattern)
			defer sub.Close()

			if got := s.Publish(tt.channel, "p"); got != tt.wantCount {
				t.Fatalf("Publish count = %d, want %d", got, tt.wantCount)
			}
			msgs := drainMessages(sub)
			if tt.wantCount == 1 {
				if len(msgs) != 1 {
					t.Fatalf("got %d messages, want 1", len(msgs))
				}
				want := Message{Channel: tt.channel, Pattern: tt.pattern, Payload: "p"}
				if msgs[0] != want {
					t.Fatalf("message = %+v, want %+v", msgs[0], want)
				}
			} else if len(msgs) != 0 {
				t.Fatalf("got %d messages, want 0", len(msgs))
			}
		})
	}
}

func TestPublishCountsExactAndPattern(t *testing.T) {
	s := New()
	exact := s.Subscribe("news.tech")
	pat := s.PSubscribe("news.*")
	defer exact.Close()
	defer pat.Close()

	if got := s.Publish("news.tech", "v"); got != 2 {
		t.Fatalf("Publish count = %d, want 2", got)
	}
	if em := drainMessages(exact); len(em) != 1 || em[0].Pattern != "" {
		t.Fatalf("exact subscriber messages = %+v", em)
	}
	if pm := drainMessages(pat); len(pm) != 1 || pm[0].Pattern != "news.*" {
		t.Fatalf("pattern subscriber messages = %+v", pm)
	}
}

func TestSubscriptionChannels(t *testing.T) {
	s := New()
	sub := s.Subscribe("b", "a", "b", "c")
	defer sub.Close()

	got := sub.Channels()
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Channels() = %v, want %v", got, want)
	}

	psub := s.PSubscribe("p.*")
	defer psub.Close()
	if got := psub.Channels(); len(got) != 0 {
		t.Fatalf("pattern subscription Channels() = %v, want empty", got)
	}
}

func TestNonBlockingDeliveryDropsWhenFull(t *testing.T) {
	s := New()
	sub := s.Subscribe("c")
	defer sub.Close()

	total := pubsubBufferSize + 5
	for i := 0; i < total; i++ {
		if got := s.Publish("c", "v"); got != 1 {
			t.Fatalf("Publish count = %d, want 1", got)
		}
	}
	// Buffer holds at most pubsubBufferSize; extra sends are dropped, and
	// Publish never blocks.
	msgs := drainMessages(sub)
	if len(msgs) != pubsubBufferSize {
		t.Fatalf("buffered messages = %d, want %d", len(msgs), pubsubBufferSize)
	}
}

func TestCloseIsIdempotentAndUnregisters(t *testing.T) {
	s := New()
	sub := s.Subscribe("c")

	sub.Close()
	sub.Close() // second call must be a no-op, not panic

	if got := s.Publish("c", "v"); got != 0 {
		t.Fatalf("Publish count after close = %d, want 0", got)
	}
	if _, ok := <-sub.Messages(); ok {
		t.Fatalf("Messages channel should be closed after Close")
	}
	if chans := s.PubSubChannels(""); len(chans) != 0 {
		t.Fatalf("PubSubChannels after close = %v, want empty", chans)
	}
}

func TestPubSubChannels(t *testing.T) {
	s := New()
	defer s.Subscribe("news.tech", "news.sport", "weather").Close()

	tests := []struct {
		name    string
		pattern string
		want    []string
	}{
		{"empty lists all", "", []string{"news.sport", "news.tech", "weather"}},
		{"star lists all", "*", []string{"news.sport", "news.tech", "weather"}},
		{"filtered", "news.*", []string{"news.sport", "news.tech"}},
		{"no match", "db.*", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.PubSubChannels(tt.pattern)
			sort.Strings(got)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("PubSubChannels(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

func TestPubSubNumSub(t *testing.T) {
	s := New()
	a := s.Subscribe("c1")
	b := s.Subscribe("c1")
	c := s.Subscribe("c2")
	defer a.Close()
	defer b.Close()
	defer c.Close()

	got := s.PubSubNumSub("c1", "c2", "absent")
	want := map[string]int{"c1": 2, "c2": 1, "absent": 0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PubSubNumSub = %v, want %v", got, want)
	}
}

func TestPubSubNumPat(t *testing.T) {
	s := New()
	if got := s.PubSubNumPat(); got != 0 {
		t.Fatalf("PubSubNumPat empty = %d, want 0", got)
	}
	p1 := s.PSubscribe("a.*")
	p2 := s.PSubscribe("b.*")
	dup := s.PSubscribe("a.*") // same pattern, distinct patterns stays 2
	defer p1.Close()
	defer p2.Close()
	defer dup.Close()

	if got := s.PubSubNumPat(); got != 2 {
		t.Fatalf("PubSubNumPat = %d, want 2", got)
	}
	p1.Close()
	if got := s.PubSubNumPat(); got != 2 {
		t.Fatalf("PubSubNumPat after closing one of two on a.* = %d, want 2", got)
	}
	dup.Close()
	if got := s.PubSubNumPat(); got != 1 {
		t.Fatalf("PubSubNumPat after closing all a.* = %d, want 1", got)
	}
}

func TestStoresAreIsolated(t *testing.T) {
	s1 := New()
	s2 := New()
	sub := s1.Subscribe("c")
	defer sub.Close()

	if got := s2.Publish("c", "v"); got != 0 {
		t.Fatalf("cross-store Publish count = %d, want 0", got)
	}
	if got := s1.Publish("c", "v"); got != 1 {
		t.Fatalf("same-store Publish count = %d, want 1", got)
	}
}
