package glpi

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
)

// Error carries safe, structured failure details. It never includes URLs,
// credentials, response bodies, or tokens.
type Error struct {
	Op         string
	Kind       error
	StatusCode int
	RetryAfter time.Duration
	Field      string
}

func (e *Error) Error() string {
	if e == nil {
		return "glpi: unknown error"
	}
	msg := "glpi"
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
