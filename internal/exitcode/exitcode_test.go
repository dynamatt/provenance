package exitcode

import (
	"errors"
	"fmt"
	"testing"
)

func TestOf(t *testing.T) {
	base := errors.New("boom")
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, OK},
		{"plain error", base, Error},
		{"usage", Usage(base), Error},
		{"not implemented", &NotImplementedError{Command: "export"}, Error},
		{"failure", Failed(base), Failure},
		{"wrapped failure", fmt.Errorf("context: %w", Failed(base)), Failure},
	}
	for _, tc := range cases {
		if got := Of(tc.err); got != tc.want {
			t.Errorf("%s: Of() = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestWrappersKeepMessageAndCause(t *testing.T) {
	base := errors.New("boom")
	for _, err := range []error{Usage(base), Failed(base)} {
		if err.Error() != "boom" || !errors.Is(err, base) {
			t.Errorf("%T: message %q, Is(base) = %v", err, err.Error(), errors.Is(err, base))
		}
	}
	if Usage(nil) != nil || Failed(nil) != nil {
		t.Error("wrapping nil should return nil")
	}
	if !IsUsage(fmt.Errorf("x: %w", Usage(base))) || IsUsage(base) {
		t.Error("IsUsage misclassifies")
	}
}

func TestNotImplementedMessage(t *testing.T) {
	err := &NotImplementedError{Command: "sign verify"}
	if got, want := err.Error(), "sign verify: not implemented yet"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
