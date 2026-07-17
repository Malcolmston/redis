package redis

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// RESP is the REdis Serialization Protocol. This file provides encoders and
// decoders for the five RESP2 types:
//
//	+  simple string
//	-  error
//	:  integer
//	$  bulk string (and the null bulk string)
//	*  array (and the null array)

// SimpleString is a RESP simple string (the "+OK" form). It is a distinct type
// so encoders can tell it apart from a bulk string.
type SimpleString string

// RESPError is a RESP error reply (the "-ERR ..." form).
type RESPError string

// Error implements the error interface.
func (e RESPError) Error() string { return string(e) }

// Encoder writes RESP values to an underlying writer.
type Encoder struct {
	w *bufio.Writer
}

// NewEncoder returns an Encoder writing to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: bufio.NewWriter(w)}
}

// Encode writes v as a RESP value and flushes. Supported dynamic types:
// SimpleString, RESPError, error, nil, int/int64/int, string ([]byte) as bulk
// string, and []any as an array. Nested arrays are supported.
func (e *Encoder) Encode(v any) error {
	if err := e.encode(v); err != nil {
		return err
	}
	return e.w.Flush()
}

func (e *Encoder) encode(v any) error {
	switch val := v.(type) {
	case nil:
		_, err := e.w.WriteString("$-1\r\n")
		return err
	case SimpleString:
		return e.writeLine('+', string(val))
	case RESPError:
		return e.writeLine('-', string(val))
	case error:
		return e.writeLine('-', val.Error())
	case bool:
		n := 0
		if val {
			n = 1
		}
		return e.writeLine(':', strconv.Itoa(n))
	case int:
		return e.writeLine(':', strconv.Itoa(val))
	case int64:
		return e.writeLine(':', strconv.FormatInt(val, 10))
	case string:
		return e.writeBulk(val)
	case []byte:
		return e.writeBulk(string(val))
	case []string:
		if err := e.writeLine('*', strconv.Itoa(len(val))); err != nil {
			return err
		}
		for _, s := range val {
			if err := e.writeBulk(s); err != nil {
				return err
			}
		}
		return nil
	case []any:
		if val == nil {
			_, err := e.w.WriteString("*-1\r\n")
			return err
		}
		if err := e.writeLine('*', strconv.Itoa(len(val))); err != nil {
			return err
		}
		for _, elem := range val {
			if err := e.encode(elem); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("resp: cannot encode %T", v)
	}
}

func (e *Encoder) writeLine(prefix byte, body string) error {
	if err := e.w.WriteByte(prefix); err != nil {
		return err
	}
	if _, err := e.w.WriteString(body); err != nil {
		return err
	}
	_, err := e.w.WriteString("\r\n")
	return err
}

func (e *Encoder) writeBulk(s string) error {
	if err := e.writeLine('$', strconv.Itoa(len(s))); err != nil {
		return err
	}
	if _, err := e.w.WriteString(s); err != nil {
		return err
	}
	_, err := e.w.WriteString("\r\n")
	return err
}

// Decoder reads RESP values from an underlying reader.
type Decoder struct {
	r *bufio.Reader
}

// NewDecoder returns a Decoder reading from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: bufio.NewReader(r)}
}

// ErrProtocol indicates malformed RESP input.
var ErrProtocol = errors.New("resp: protocol error")

// Decode reads the next RESP value. The concrete Go types returned are:
//
//	simple string -> SimpleString
//	error         -> RESPError
//	integer       -> int64
//	bulk string   -> string, or nil for the null bulk string
//	array         -> []any, or nil for the null array
func (d *Decoder) Decode() (any, error) {
	prefix, err := d.r.ReadByte()
	if err != nil {
		return nil, err
	}
	line, err := d.readLine()
	if err != nil {
		return nil, err
	}
	switch prefix {
	case '+':
		return SimpleString(line), nil
	case '-':
		return RESPError(line), nil
	case ':':
		n, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return nil, ErrProtocol
		}
		return n, nil
	case '$':
		return d.decodeBulk(line)
	case '*':
		return d.decodeArray(line)
	default:
		return nil, ErrProtocol
	}
}

func (d *Decoder) decodeBulk(line string) (any, error) {
	n, err := strconv.Atoi(line)
	if err != nil {
		return nil, ErrProtocol
	}
	if n < 0 {
		return nil, nil
	}
	buf := make([]byte, n+2) // include trailing CRLF
	if _, err := io.ReadFull(d.r, buf); err != nil {
		return nil, err
	}
	if buf[n] != '\r' || buf[n+1] != '\n' {
		return nil, ErrProtocol
	}
	return string(buf[:n]), nil
}

func (d *Decoder) decodeArray(line string) (any, error) {
	n, err := strconv.Atoi(line)
	if err != nil {
		return nil, ErrProtocol
	}
	if n < 0 {
		return nil, nil
	}
	arr := make([]any, n)
	for i := 0; i < n; i++ {
		v, err := d.Decode()
		if err != nil {
			return nil, err
		}
		arr[i] = v
	}
	return arr, nil
}

// readLine reads through the next CRLF and returns the line without the CRLF.
func (d *Decoder) readLine() (string, error) {
	line, err := d.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return "", ErrProtocol
	}
	return line[:len(line)-2], nil
}

// DecodeCommand reads a single client command, which RESP encodes as an array
// of bulk strings, and returns it as a slice of argument strings. Inline
// commands are not supported.
func (d *Decoder) DecodeCommand() ([]string, error) {
	v, err := d.Decode()
	if err != nil {
		return nil, err
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, ErrProtocol
	}
	args := make([]string, len(arr))
	for i, e := range arr {
		s, ok := e.(string)
		if !ok {
			return nil, ErrProtocol
		}
		args[i] = s
	}
	return args, nil
}
