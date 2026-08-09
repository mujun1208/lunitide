package networkpolicy

import (
	"errors"
	"fmt"
)

type Code string

const (
	CodeSSRFBlocked       Code = "SSRF_BLOCKED"
	CodeDNSError          Code = "DNS_ERROR"
	CodeTLSError          Code = "TLS_ERROR"
	CodeConnectionRefused Code = "CONNECTION_REFUSED"
	CodeTimeout           Code = "TIMEOUT"
	CodeResponseTooLarge  Code = "RESPONSE_TOO_LARGE"
	CodeRedirectBlocked   Code = "REDIRECT_BLOCKED"
	CodeCancelled         Code = "CANCELLED"
)

// Error deliberately contains no request URL or credentials.
type Error struct {
	Code Code
	Op   string
	Err  error
}

func (e *Error) Error() string {
	if e.Op == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Op, e.Code)
}
func (e *Error) Unwrap() error { return e.Err }

func ErrorCode(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
