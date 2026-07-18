package redis

import (
	"reflect"
	"testing"
)

func TestSMIsMember(t *testing.T) {
	s := New()
	s.SAdd("k", "a", "b", "c")
	got, err := s.SMIsMember("k", "a", "x", "c")
	if err != nil {
		t.Fatalf("SMIsMember: %v", err)
	}
	if !reflect.DeepEqual(got, []bool{true, false, true}) {
		t.Fatalf("SMIsMember = %v", got)
	}
	got, _ = s.SMIsMember("missing", "a")
	if !reflect.DeepEqual(got, []bool{false}) {
		t.Fatalf("SMIsMember missing = %v", got)
	}
	s.Set("str", "v", SetOptions{})
	if _, err := s.SMIsMember("str", "a"); err != ErrWrongType {
		t.Fatalf("SMIsMember wrong type = %v", err)
	}
}

func TestSInterCard(t *testing.T) {
	s := New()
	s.SAdd("a", "1", "2", "3", "4")
	s.SAdd("b", "2", "3", "4", "5")
	s.SAdd("c", "3", "4", "6")

	if n, err := s.SInterCard(0, "a", "b"); err != nil || n != 3 {
		t.Fatalf("SInterCard(a,b) = %d, %v; want 3", n, err)
	}
	if n, err := s.SInterCard(0, "a", "b", "c"); err != nil || n != 2 {
		t.Fatalf("SInterCard(a,b,c) = %d, %v; want 2", n, err)
	}
	if n, err := s.SInterCard(1, "a", "b"); err != nil || n != 1 {
		t.Fatalf("SInterCard limit 1 = %d, %v; want 1", n, err)
	}
	if n, err := s.SInterCard(0, "a", "missing"); err != nil || n != 0 {
		t.Fatalf("SInterCard with missing = %d, %v; want 0", n, err)
	}
}
