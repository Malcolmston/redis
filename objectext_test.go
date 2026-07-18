package redis

import "testing"

func TestObjectAccessors(t *testing.T) {
	s := New()
	s.Set("n", "12345", SetOptions{})
	s.Set("str", "hello world", SetOptions{})
	s.RPush("l", "a", "b")

	if enc, ok := s.ObjectEncoding("n"); !ok || enc != "int" {
		t.Fatalf("encoding n = %q, %v; want int", enc, ok)
	}
	if enc, ok := s.ObjectEncoding("str"); !ok || enc != "embstr" {
		t.Fatalf("encoding str = %q, %v", enc, ok)
	}
	if enc, ok := s.ObjectEncoding("l"); !ok || enc != "listpack" {
		t.Fatalf("encoding l = %q, %v", enc, ok)
	}
	if _, ok := s.ObjectEncoding("missing"); ok {
		t.Fatalf("encoding of missing key returned ok")
	}
	if rc, ok := s.ObjectRefCount("n"); !ok || rc != 1 {
		t.Fatalf("refcount = %d, %v", rc, ok)
	}
	if it, ok := s.ObjectIdleTime("n"); !ok || it != 0 {
		t.Fatalf("idletime = %d, %v", it, ok)
	}
	if f, ok := s.ObjectFreq("n"); !ok || f != 0 {
		t.Fatalf("freq = %d, %v", f, ok)
	}
}

func TestMemoryUsage(t *testing.T) {
	s := New()
	s.Set("k", "hello", SetOptions{})
	// overhead(16) + len("k")(1) + len("hello")(5) = 22.
	if m, ok := s.MemoryUsage("k"); !ok || m != 22 {
		t.Fatalf("MemoryUsage = %d, %v; want 22", m, ok)
	}
	if _, ok := s.MemoryUsage("missing"); ok {
		t.Fatalf("MemoryUsage of missing key returned ok")
	}

	// Larger content uses more memory.
	s.RPush("l", "aa", "bbb")
	small, _ := s.MemoryUsage("k")
	big, _ := s.MemoryUsage("l")
	if big <= small {
		t.Fatalf("list usage %d not greater than string usage %d", big, small)
	}
}
