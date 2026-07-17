package redis

import (
	"net"
	"testing"
	"time"
)

func TestServerRESP(t *testing.T) {
	s := New()
	srv := NewServer(s)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	for i := 0; i < 100 && srv.Addr() == nil; i++ {
		time.Sleep(time.Millisecond)
	}
	if srv.Addr() == nil {
		t.Fatal("expected non-nil addr")
	}

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	enc := NewEncoder(conn)
	dec := NewDecoder(conn)

	if err := enc.Encode([]any{"SET", "k", "v"}); err != nil {
		t.Fatal(err)
	}
	reply, err := dec.Decode()
	if err != nil || reply != SimpleString("OK") {
		t.Fatalf("SET reply = %v %v", reply, err)
	}

	_ = enc.Encode([]any{"GET", "k"})
	reply, err = dec.Decode()
	if err != nil || reply != "v" {
		t.Fatalf("GET reply = %v %v", reply, err)
	}

	// Error reply for unknown command.
	_ = enc.Encode([]any{"BOGUS"})
	reply, err = dec.Decode()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reply.(RESPError); !ok {
		t.Fatalf("expected RESPError, got %T", reply)
	}
}

func TestServerCloseIdempotent(t *testing.T) {
	srv := NewServer(New())
	if err := srv.Close(); err != nil {
		t.Fatalf("close unstarted = %v", err)
	}
	// Second close returns stored error (nil).
	if err := srv.Close(); err != nil {
		t.Fatalf("second close = %v", err)
	}
}
