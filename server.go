package redis

import (
	"errors"
	"io"
	"net"
	"sync"
)

// Server is a minimal TCP server that speaks RESP and dispatches each command
// to a backing Store via Do. It is intended for integration use and is kept off
// the deterministic test path. The zero value is not usable; use NewServer.
type Server struct {
	store *Store

	mu       sync.Mutex
	ln       net.Listener
	closed   bool
	closeErr error
}

// NewServer returns a Server that dispatches commands to store.
func NewServer(store *Store) *Server {
	return &Server{store: store}
}

// ListenAndServe binds to addr (e.g. ":6379") and serves connections until the
// listener is closed. It always returns a non-nil error.
func (srv *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return srv.Serve(ln)
}

// Serve accepts connections on ln and serves each in its own goroutine. Serve
// returns when the listener is closed via Close.
func (srv *Server) Serve(ln net.Listener) error {
	srv.mu.Lock()
	if srv.closed {
		srv.mu.Unlock()
		_ = ln.Close()
		return net.ErrClosed
	}
	srv.ln = ln
	srv.mu.Unlock()

	var wg sync.WaitGroup
	for {
		conn, err := ln.Accept()
		if err != nil {
			srv.mu.Lock()
			closed := srv.closed
			srv.mu.Unlock()
			if closed {
				wg.Wait()
				return net.ErrClosed
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			wg.Wait()
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.handleConn(conn)
		}()
	}
}

// Addr returns the address the server is listening on, or nil if not serving.
func (srv *Server) Addr() net.Addr {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.ln == nil {
		return nil
	}
	return srv.ln.Addr()
}

// Close stops the server and closes its listener.
func (srv *Server) Close() error {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.closed {
		return srv.closeErr
	}
	srv.closed = true
	if srv.ln != nil {
		srv.closeErr = srv.ln.Close()
	}
	return srv.closeErr
}

// handleConn reads RESP command arrays and writes RESP replies until the client
// disconnects or a protocol error occurs.
func (srv *Server) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	dec := NewDecoder(conn)
	enc := NewEncoder(conn)
	for {
		args, err := dec.DecodeCommand()
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				_ = enc.Encode(RESPError("ERR " + err.Error()))
			}
			return
		}
		reply, cmdErr := srv.store.Do(args...)
		if cmdErr != nil {
			if encErr := enc.Encode(RESPError(cmdErr.Error())); encErr != nil {
				return
			}
			continue
		}
		if encErr := enc.Encode(reply); encErr != nil {
			return
		}
	}
}
