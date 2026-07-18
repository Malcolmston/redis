package redis

import (
	"bytes"
	"testing"
	"time"
)

func populateSnapshotStore(s *Store) {
	s.Set("str", "hello", SetOptions{})
	s.RPush("list", "a", "b", "c")
	s.HSet("hash", "f1", "v1", "f2", "v2")
	s.SAdd("set", "x", "y", "z")
	s.ZAdd("zset", ZMember{"m1", 1.5}, ZMember{"m2", 2.5})
}

func TestSnapshotRoundTrip(t *testing.T) {
	s := New()
	populateSnapshotStore(s)

	data, err := s.MarshalSnapshot()
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}

	got, err := NewFromSnapshot(data)
	if err != nil {
		t.Fatalf("NewFromSnapshot: %v", err)
	}

	if v, _, _ := got.Get("str"); v != "hello" {
		t.Fatalf("str = %q", v)
	}
	if l, _ := got.LRange("list", 0, -1); len(l) != 3 || l[0] != "a" || l[2] != "c" {
		t.Fatalf("list = %v", l)
	}
	if v, ok, _ := got.HGet("hash", "f2"); !ok || v != "v2" {
		t.Fatalf("hash f2 = %q, %v", v, ok)
	}
	if ok, _ := got.SIsMember("set", "y"); !ok {
		t.Fatalf("set missing y")
	}
	if sc, ok, _ := got.ZScore("zset", "m2"); !ok || sc != 2.5 {
		t.Fatalf("zset m2 = %v, %v", sc, ok)
	}
}

func TestSnapshotDeterministic(t *testing.T) {
	s1 := New()
	s2 := New()
	populateSnapshotStore(s1)
	// Insert in a different order into s2.
	s2.ZAdd("zset", ZMember{"m2", 2.5}, ZMember{"m1", 1.5})
	s2.SAdd("set", "z", "y", "x")
	s2.HSet("hash", "f2", "v2", "f1", "v1")
	s2.RPush("list", "a", "b", "c")
	s2.Set("str", "hello", SetOptions{})

	d1, _ := s1.MarshalSnapshot()
	d2, _ := s2.MarshalSnapshot()
	if !bytes.Equal(d1, d2) {
		t.Fatalf("snapshots not deterministic")
	}
}

func TestSnapshotTTL(t *testing.T) {
	clk := NewManualClock(time.Unix(1000, 0))
	s := NewWithClock(clk)
	s.Set("k", "v", SetOptions{EX: 100 * time.Second})

	data, _ := s.MarshalSnapshot()

	clk2 := NewManualClock(time.Unix(1000, 0))
	restored := NewWithClock(clk2)
	if err := restored.LoadSnapshot(data); err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if _, code := restored.TTL("k"); code != TTLValue {
		t.Fatalf("restored TTL code = %v", code)
	}
	clk2.Advance(101 * time.Second)
	if _, ok, _ := restored.Get("k"); ok {
		t.Fatalf("restored key should have expired")
	}
}

func TestDumpRestore(t *testing.T) {
	s := New()
	s.ZAdd("z", ZMember{"a", 1}, ZMember{"b", 2})

	data, ok, err := s.DumpKey("z")
	if err != nil || !ok {
		t.Fatalf("DumpKey = %v, %v", ok, err)
	}
	if _, ok, _ := s.DumpKey("missing"); ok {
		t.Fatalf("DumpKey of missing returned ok")
	}

	dst := New()
	if err := dst.RestoreKey("z2", data, 0, false); err != nil {
		t.Fatalf("RestoreKey: %v", err)
	}
	if sc, ok, _ := dst.ZScore("z2", "b"); !ok || sc != 2 {
		t.Fatalf("restored z2 b = %v, %v", sc, ok)
	}

	// BUSYKEY without replace.
	if err := dst.RestoreKey("z2", data, 0, false); err != ErrBusyKey {
		t.Fatalf("RestoreKey busy = %v, want ErrBusyKey", err)
	}
	// replace=true overwrites.
	if err := dst.RestoreKey("z2", data, 0, true); err != nil {
		t.Fatalf("RestoreKey replace: %v", err)
	}
}

func TestSnapshotBadData(t *testing.T) {
	if _, err := NewFromSnapshot([]byte("garbage")); err != ErrBadSnapshot {
		t.Fatalf("bad snapshot = %v, want ErrBadSnapshot", err)
	}
	s := New()
	if err := s.RestoreKey("k", []byte{0xff, 0xff}, 0, false); err != ErrBadSnapshot {
		t.Fatalf("bad restore = %v, want ErrBadSnapshot", err)
	}
}

func BenchmarkMarshalSnapshot(b *testing.B) {
	s := New()
	for i := 0; i < 100; i++ {
		s.Set(string(rune('a'+i%26))+string(rune('0'+i%10)), "value", SetOptions{})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.MarshalSnapshot()
	}
}
