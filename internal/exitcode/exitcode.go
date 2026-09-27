// Package exitcode is the single place that maps command outcomes to process
// exit codes, per the contract in Detailed Design §2:
//
//	0  success
//	1  expected failure condition reported (violations, mismatches, gate blocked)
//	2  tool or usage error (bad arguments, missing file, I/O failure)
//
// Commands return an error; only errors built with Failure map to 1. Every
// other error — including argument and flag errors raised by the CLI
// framework — maps to 2, so an unclassified error can never be mistaken for a
// reported result.
package exitcode

import "errors"

const (
	OK      = 0
	Failure = 1
	Error   = 2
)

// failureError marks an expected failure condition that the command has
// already reported (exit 1).
type failureError struct{ err error }

func (e *failureError) Error() string { return e.err.Error() }
func (e *failureError) Unwrap() error { return e.err }

// Failed wraps err as an expected failure condition (exit 1).
func Failed(err error) error {
	if err == nil {
		return nil
	}
	return &failureError{err: err}
}

// usageError marks a problem with how the command was invoked (exit 2). The
// CLI prints a pointer to --help after the message.
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// Usage wraps err as a usage error (exit 2).
func Usage(err error) error {
	if err == nil {
		return nil
	}
	return &usageError{err: err}
}

// IsUsage reports whether err is, or wraps, a usage error.
func IsUsage(err error) bool {
	var u *usageError
	return errors.As(err, &u)
}

// NotImplementedError is returned by commands that exist in the CLI surface
// but have no implementation in this build (exit 2).
type NotImplementedError struct {
	// Command is the command path without the binary name, e.g. "sign verify".
	Command string
}

func (e *NotImplementedError) Error() string { return e.Command + ": not implemented yet" }

// Of returns the exit code for the outcome of a command.
func Of(err error) int {
	if err == nil {
		return OK
	}
	var f *failureError
	if errors.As(err, &f) {
		return Failure
	}
	return Error
}
