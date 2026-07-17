package redis

import "errors"

// Errors returned by store commands. They mirror common Redis error replies and
// are safe to compare with errors.Is.
var (
	// ErrWrongType is returned when a command is applied to a key holding a
	// value of a different type.
	ErrWrongType = errors.New("WRONGTYPE Operation against a key holding the wrong kind of value")
	// ErrNotInteger is returned when a numeric operation is attempted on a
	// value that is not a base-10 integer.
	ErrNotInteger = errors.New("ERR value is not an integer or out of range")
	// ErrNotFloat is returned when a float operation receives a non-float
	// argument.
	ErrNotFloat = errors.New("ERR value is not a valid float")
	// ErrSyntax is returned when command options are malformed.
	ErrSyntax = errors.New("ERR syntax error")
	// ErrWrongArgs is returned when a command receives the wrong number of
	// arguments.
	ErrWrongArgs = errors.New("ERR wrong number of arguments")
	// ErrUnknownCommand is returned by Do for an unrecognized command.
	ErrUnknownCommand = errors.New("ERR unknown command")
	// ErrOutOfRange is returned when an index argument is out of range.
	ErrOutOfRange = errors.New("ERR index out of range")
)
