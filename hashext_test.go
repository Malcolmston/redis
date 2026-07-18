package redis

import "testing"

func TestHGetDel(t *testing.T) {
	s := New()
	s.HSet("h", "a", "1", "b", "2", "c", "3")
	got, err := s.HGetDel("h", "a", "x", "c")
	if err != nil {
		t.Fatalf("HGetDel: %v", err)
	}
	if got[0] == nil || *got[0] != "1" {
		t.Fatalf("field a = %v", got[0])
	}
	if got[1] != nil {
		t.Fatalf("field x should be nil, got %v", *got[1])
	}
	if got[2] == nil || *got[2] != "3" {
		t.Fatalf("field c = %v", got[2])
	}
	if ok, _ := s.HExists("h", "a"); ok {
		t.Fatalf("field a not deleted")
	}
	if n, _ := s.HLen("h"); n != 1 {
		t.Fatalf("HLen after HGetDel = %d; want 1", n)
	}
	// Deleting the rest removes the key.
	s.HGetDel("h", "b")
	if s.Exists("h") != 0 {
		t.Fatalf("emptied hash not deleted")
	}
}

func TestHGetDelWrongType(t *testing.T) {
	s := New()
	s.Set("k", "v", SetOptions{})
	if _, err := s.HGetDel("k", "a"); err != ErrWrongType {
		t.Fatalf("HGetDel wrong type = %v", err)
	}
}

func TestHRandFieldWithValues(t *testing.T) {
	s := New()
	s.HSet("h", "a", "1", "b", "2", "c", "3")
	values := map[string]string{"a": "1", "b": "2", "c": "3"}

	got, err := s.HRandFieldWithValues("h", 2)
	if err != nil || len(got) != 2 {
		t.Fatalf("HRandFieldWithValues = %v, %v", got, err)
	}
	seen := map[string]bool{}
	for _, e := range got {
		if values[e.Field] != e.Value {
			t.Fatalf("bad pair %+v", e)
		}
		if seen[e.Field] {
			t.Fatalf("duplicate field %q", e.Field)
		}
		seen[e.Field] = true
	}
	if got, _ := s.HRandFieldWithValues("h", 10); len(got) != 3 {
		t.Fatalf("cap = %v", got)
	}
	if got, _ := s.HRandFieldWithValues("h", -5); len(got) != 5 {
		t.Fatalf("negative = %v", got)
	}
}
