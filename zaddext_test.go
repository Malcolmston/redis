package redis

import "testing"

func TestZAddWith(t *testing.T) {
	s := New()
	s.ZAdd("z", ZMember{"a", 5})

	// NX does not update an existing member.
	if n, err := s.ZAddWith("z", ZAddOptions{NX: true}, ZMember{"a", 10}, ZMember{"b", 2}); err != nil || n != 1 {
		t.Fatalf("ZAddWith NX = %d, %v; want 1", n, err)
	}
	if sc, _, _ := s.ZScore("z", "a"); sc != 5 {
		t.Fatalf("NX updated existing: a = %v", sc)
	}

	// XX does not add a new member.
	if n, err := s.ZAddWith("z", ZAddOptions{XX: true}, ZMember{"c", 3}); err != nil || n != 0 {
		t.Fatalf("ZAddWith XX add = %d, %v; want 0", n, err)
	}
	if s.Exists("z") == 0 {
		t.Fatalf("z vanished")
	}

	// GT only raises the score.
	if _, err := s.ZAddWith("z", ZAddOptions{GT: true}, ZMember{"a", 3}); err != nil {
		t.Fatalf("GT: %v", err)
	}
	if sc, _, _ := s.ZScore("z", "a"); sc != 5 {
		t.Fatalf("GT lowered score: %v", sc)
	}
	if _, err := s.ZAddWith("z", ZAddOptions{GT: true}, ZMember{"a", 9}); err != nil {
		t.Fatalf("GT raise: %v", err)
	}
	if sc, _, _ := s.ZScore("z", "a"); sc != 9 {
		t.Fatalf("GT did not raise: %v", sc)
	}

	// CH counts updates.
	n, err := s.ZAddWith("z", ZAddOptions{CH: true}, ZMember{"a", 1}, ZMember{"d", 4})
	if err != nil || n != 2 {
		t.Fatalf("ZAddWith CH = %d, %v; want 2", n, err)
	}

	// Contradictory options error.
	if _, err := s.ZAddWith("z", ZAddOptions{NX: true, XX: true}, ZMember{"x", 1}); err != ErrSyntax {
		t.Fatalf("NX+XX = %v, want ErrSyntax", err)
	}
	if _, err := s.ZAddWith("z", ZAddOptions{GT: true, LT: true}, ZMember{"x", 1}); err != ErrSyntax {
		t.Fatalf("GT+LT = %v, want ErrSyntax", err)
	}
}
