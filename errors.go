package supermarket

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors for use with errors.Is. Supermarket returns 400 for
// "not found" rather than 404; Unwrap normalizes both into ErrNotFound
// using the response's error_code field.
var (
	ErrNotFound             = errors.New("supermarket: not found")
	ErrUnauthorized         = errors.New("supermarket: unauthorized")
	ErrForbidden            = errors.New("supermarket: forbidden")
	ErrInvalidData          = errors.New("supermarket: invalid data")
	ErrUnauthenticatedWrite = errors.New("supermarket: write requires Username and Key")
	// ErrInvalidArgument is returned locally, without contacting the server,
	// when a caller passes a blank value for a required path segment such as
	// a cookbook name or version.
	ErrInvalidArgument = errors.New("supermarket: invalid argument")
)

// ErrorResponse describes a non-2xx response from Supermarket.
type ErrorResponse struct {
	Method     string   // request method
	Path       string   // request path
	StatusCode int      // HTTP status code
	Code       string   // server-supplied error_code (e.g. NOT_FOUND)
	Messages   []string // server-supplied error_messages
}

func (e *ErrorResponse) Error() string {
	msg := strings.Join(e.Messages, "; ")
	if msg == "" {
		msg = "(no message)"
	}
	if e.Code != "" {
		return fmt.Sprintf("supermarket: %s %s: %d %s: %s",
			e.Method, e.Path, e.StatusCode, e.Code, msg)
	}
	return fmt.Sprintf("supermarket: %s %s: %d: %s",
		e.Method, e.Path, e.StatusCode, msg)
}

// Unwrap maps the error_code (preferred) or HTTP status to a sentinel
// error for errors.Is.
func (e *ErrorResponse) Unwrap() error {
	switch e.Code {
	case "NOT_FOUND":
		return ErrNotFound
	case "UNAUTHORIZED":
		return ErrUnauthorized
	case "FORBIDDEN":
		return ErrForbidden
	case "INVALID_DATA":
		return ErrInvalidData
	}
	switch e.StatusCode {
	case 401:
		return ErrUnauthorized
	case 403:
		return ErrForbidden
	case 404:
		return ErrNotFound
	}
	return nil
}

// newErrorResponse decodes Supermarket's `{error_messages,error_code}`
// envelope. When the body is not the expected shape it falls back to
// the raw body as a single message.
func newErrorResponse(method, path string, status int, body []byte) *ErrorResponse {
	er := &ErrorResponse{Method: method, Path: path, StatusCode: status}
	var parsed struct {
		ErrorMessages []string `json:"error_messages"`
		ErrorCode     string   `json:"error_code"`
	}
	if json.Unmarshal(body, &parsed) == nil {
		er.Messages = parsed.ErrorMessages
		er.Code = parsed.ErrorCode
	}
	if len(er.Messages) == 0 && len(body) > 0 {
		er.Messages = []string{strings.TrimSpace(string(body))}
	}
	return er
}
