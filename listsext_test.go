package redis

import (
	"reflect"
	"testing"
)

func TestLPushXRPushX(t *testing.T) {
	s := New()
	// No key yet: X variants are no-ops.
	if n, err := s.LPushX("k", "a"); err != nil || n != 0 {
		t.Fatalf("LPushX on missing = %d, %v", n, err)
	}
	if s.Exists("k") != 0 {
		t.Fatalf("LPushX created a key")
	}
	s.RPush("k", "b")
	if n, err := s.RPushX("k", "c"); err != nil || n != 2 {
		t.Fatalf("RPushX = %d, %v; want 2", n, err)
	}
	if n, err := s.LPushX("k", "a"); err != nil || n != 3 {
		t.Fatalf("LPushX = %d, %v; want 3", n, err)
	}
	got, _ := s.LRange("k", 0, -1)
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("list = %v", got)
	}
}

func TestLPopNRPopN(t *testing.T) {
	s := New()
	s.RPush("k", "a", "b", "c", "d")
	got, err := s.LPopN("k", 2)
	if err != nil || !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("LPopN = %v, %v", got, err)
	}
	got, err = s.RPopN("k", 5)
	if err != nil || !reflect.DeepEqual(got, []string{"d", "c"}) {
		t.Fatalf("RPopN = %v, %v", got, err)
	}
	if s.Exists("k") != 0 {
		t.Fatalf("emptied list not deleted")
	}
}

func TestLMPop(t *testing.T) {
	s := New()
	s.RPush("l2", "x", "y", "z")
	key, vals, ok, err := s.LMPop(DirLeft, 2, "l1", "l2", "l3")
	if err != nil || !ok || key != "l2" {
		t.Fatalf("LMPop key = %q, ok=%v, err=%v", key, ok, err)
	}
	if !reflect.DeepEqual(vals, []string{"x", "y"}) {
		t.Fatalf("LMPop vals = %v", vals)
	}
	// Right end of remaining ["z"].
	key, vals, ok, _ = s.LMPop(DirRight, 5, "l1", "l2")
	if !ok || key != "l2" || !reflect.DeepEqual(vals, []string{"z"}) {
		t.Fatalf("LMPop 2 = %q, %v, %v", key, vals, ok)
	}
	// Nothing left.
	_, _, ok, _ = s.LMPop(DirLeft, 1, "l1", "l2")
	if ok {
		t.Fatalf("LMPop should have found nothing")
	}
}

func TestListsextWrongType(t *testing.T) {
	s := New()
	s.Set("s", "v", SetOptions{})
	if _, err := s.LPushX("s", "a"); err != ErrWrongType {
		t.Fatalf("LPushX wrong type = %v", err)
	}
	if _, err := s.LPopN("s", 1); err != ErrWrongType {
		t.Fatalf("LPopN wrong type = %v", err)
	}
}
