package tactical

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrUnavailable = errors.New("service unavailable")
	ErrAuth        = errors.New("authentication failed")
	ErrBadRequest  = errors.New("bad request")
	ErrNotFound    = errors.New("not found")
	ErrConflict    = errors.New("conflict")
	ErrRateLimited = errors.New("rate limited")
	ErrAmbiguous   = errors.New("ambiguous match")
	ErrBadResponse = errors.New("bad response")
	ErrConfig      = errors.New("invalid configuration")
	// ErrTimeout covers both the client's own configured Timeout firing and
	// a caller-supplied context deadline expiring (T-007 7.4-R2): both are
	// observably the same "the request did not finish in time" condition.
	ErrTimeout = errors.New("timeout")
	// ErrCanceled means the caller's context was canceled before the
	// request completed (T-007 7.4-R2).
	ErrCanceled = errors.New("canceled")
)

// Error carries safe failure metadata without exposing URLs, response bodies,
// or the API key.
type Error struct {
	Op         string
	Kind       error
	StatusCode int
	RetryAfter time.Duration
	Field      string
}

func (e *Error) Error() string {
	if e == nil {
		return "tactical: unknown error"
	}
	msg := "tactical"
	if e.Op != "" {
		msg += " " + e.Op
	}
	if e.Kind != nil {
		msg += ": " + e.Kind.Error()
	}
	if e.StatusCode != 0 {
		msg += fmt.Sprintf(" (HTTP %d)", e.StatusCode)
	}
	if e.Field != "" {
		msg += ": " + e.Field
	}
	return msg
}
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Kind
}
