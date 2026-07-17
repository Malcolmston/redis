package redis

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestRESPRoundTrip(t *testing.T) {
	cases := []any{
		SimpleString("OK"),
		RESPError("ERR bad"),
		int64(42),
		int64(-7),
		"bulk string",
		"",
		nil,
		[]any{"a", int64(1), SimpleString("x")},
		[]any{}, // empty array
		[]any{"nested", []any{"deep", int64(9)}},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		if err := NewEncoder(&buf).Encode(c); err != nil {
			t.Fatalf("encode %v: %v", c, err)
		}
		got, err := NewDecoder(&buf).Decode()
		if err != nil {
			t.Fatalf("decode %v: %v", c, err)
		}
		want := normalizeExpected(c)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round trip: got %#v want %#v", got, want)
		}
	}
}

// normalizeExpected maps the encoder input to what the decoder produces, since
// the decoder does not distinguish, e.g., SimpleString wrappers inside arrays
// beyond their RESP type.
func normalizeExpected(v any) any {
	switch val := v.(type) {
	case []any:
		out := make([]any, len(val))
		for i, e := range val {
			out[i] = normalizeExpected(e)
		}
		return out
	default:
		return v
	}
}

func TestRESPEncodeWireFormat(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.Encode([]any{"SET", "k", "v"}); err != nil {
		t.Fatal(err)
	}
	want := "*3\r\n$3\r\nSET\r\n$1\r\nk\r\n$1\r\nv\r\n"
	if buf.String() != want {
		t.Fatalf("wire = %q want %q", buf.String(), want)
	}
}

func TestRESPEncodeScalars(t *testing.T) {
	check := func(v any, want string) {
		t.Helper()
		var buf bytes.Buffer
		if err := NewEncoder(&buf).Encode(v); err != nil {
			t.Fatal(err)
		}
		if buf.String() != want {
			t.Fatalf("encode %v = %q want %q", v, buf.String(), want)
		}
	}
	check(SimpleString("OK"), "+OK\r\n")
	check(RESPError("ERR x"), "-ERR x\r\n")
	check(int64(10), ":10\r\n")
	check(int(3), ":3\r\n")
	check(true, ":1\r\n")
	check(false, ":0\r\n")
	check("hi", "$2\r\nhi\r\n")
	check(nil, "$-1\r\n")
	check([]byte("ab"), "$2\r\nab\r\n")
	check([]string{"a", "b"}, "*2\r\n$1\r\na\r\n$1\r\nb\r\n")
	check([]any(nil), "*-1\r\n")
}

func TestRESPDecodeNulls(t *testing.T) {
	v, err := NewDecoder(strings.NewReader("$-1\r\n")).Decode()
	if err != nil || v != nil {
		t.Fatalf("null bulk = %v %v", v, err)
	}
	v, err = NewDecoder(strings.NewReader("*-1\r\n")).Decode()
	if err != nil || v != nil {
		t.Fatalf("null array = %v %v", v, err)
	}
}

func TestDecodeCommand(t *testing.T) {
	in := "*2\r\n$4\r\nLLEN\r\n$3\r\nkey\r\n"
	args, err := NewDecoder(strings.NewReader(in)).DecodeCommand()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args, []string{"LLEN", "key"}) {
		t.Fatalf("args = %v", args)
	}
}

func TestRESPProtocolErrors(t *testing.T) {
	bad := []string{
		"!oops\r\n",       // unknown prefix
		":notanumber\r\n", // bad integer
		"$5\r\nhi\r\n",    // wrong length / short
		"*1\r\n:bad\r\n",  // bad element
		"+missingcrlf\n",  // no CR
	}
	for _, b := range bad {
		if _, err := NewDecoder(strings.NewReader(b)).Decode(); err == nil {
			t.Fatalf("expected error decoding %q", b)
		}
	}
}

func TestEncodeUnsupported(t *testing.T) {
	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(3.14); err == nil {
		t.Fatal("expected error for unsupported type")
	}
}
