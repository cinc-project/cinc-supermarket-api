package supermarket

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors for use with errors.Is. Supermarket reports a missing
// resource as 404 with error_code NOT_FOUND; Unwrap maps the error_code
// (and, failing that, the status) to ErrNotFound, so match on the sentinel
// rather than the status code.
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
	// ErrConflict matches a 409, which Supermarket returns when deleting the
	// only remaining version of a cookbook.
	ErrConflict = errors.New("supermarket: conflict")
	// ErrRateLimited matches a 429 that outlasted the client's retries.
	ErrRateLimited = errors.New("supermarket: rate limited")
)

// Reason sentinels narrow a broad sentinel to one specific failure that
// needs its own fix. Each also matches its broad sentinel, so a caller
// checking errors.Is(err, ErrUnauthorized) or errors.Is(err, ErrInvalidData)
// keeps working. They are recognised from Supermarket's own message text
// (upstream config/locales/en.yml), which this package matches so callers
// don't have to.
var (
	// ErrVersionExists matches an upload of a cookbook version that is
	// already published (400 INVALID_DATA, "nginx (1.2.0) already exists.
	// A cookbook's version number must be unique."). Bump the version in
	// metadata.rb to publish again. It also matches ErrInvalidData.
	ErrVersionExists error = &reasonError{"supermarket: cookbook version already exists", ErrInvalidData}

	// ErrUnknownUser matches an AUTHENTICATION_FAILED response for a
	// username (the X-Ops-Userid header) that has no Supermarket account
	// ("Could not find user 'name'."). It also matches ErrUnauthorized.
	ErrUnknownUser error = &reasonError{"supermarket: unknown user", ErrUnauthorized}

	// ErrNoPublicKey matches an AUTHENTICATION_FAILED response for a user
	// who exists but has no public key on file, because they have never
	// logged in to the Supermarket web UI and linked their Chef account.
	// Logging in once fixes it. It also matches ErrUnauthorized.
	ErrNoPublicKey error = &reasonError{"supermarket: no public key on file", ErrUnauthorized}

	// ErrSignatureRejected matches an AUTHENTICATION_FAILED response for a
	// well-formed signed request that failed verification ("invalid
	// public/private key pair"). Supermarket sends the same message for
	// every verification failure, so this covers a private key that doesn't
	// match the public key on file, a clock more than 15 minutes off from
	// the server's, and a body that doesn't match its content hash; the
	// response cannot tell them apart. It also matches ErrUnauthorized.
	ErrSignatureRejected error = &reasonError{"supermarket: signature rejected (key mismatch or clock skew)", ErrUnauthorized}

	// ErrInvalidSignedRequest matches an AUTHENTICATION_FAILED response for
	// a request whose signing headers Supermarket could not parse, such as
	// a missing header or an unparseable timestamp ("Authentication failed
	// due to an invalid signed request."). It also matches ErrUnauthorized.
	ErrInvalidSignedRequest error = &reasonError{"supermarket: invalid signed request", ErrUnauthorized}
)

// reasonError is a sentinel that also matches a broader sentinel.
type reasonError struct {
	msg    string
	parent error
}

func (e *reasonError) Error() string { return e.msg }
func (e *reasonError) Unwrap() error { return e.parent }

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
// error for errors.Is. When the message identifies a specific failure, it
// returns that reason sentinel (such as ErrVersionExists or ErrUnknownUser),
// which in turn matches the broad sentinel for the code.
func (e *ErrorResponse) Unwrap() error {
	if r := e.reason(); r != nil {
		return r
	}
	switch e.Code {
	case "NOT_FOUND":
		return ErrNotFound
	case "UNAUTHORIZED", "AUTHENTICATION_FAILED":
		return ErrUnauthorized
	case "FORBIDDEN":
		return ErrForbidden
	case "INVALID_DATA", "MISSING_REQUIRED_DATA":
		return ErrInvalidData
	case "CONFLICT":
		return ErrConflict
	}
	switch e.StatusCode {
	case 401:
		return ErrUnauthorized
	case 403:
		return ErrForbidden
	case 404:
		return ErrNotFound
	case 409:
		return ErrConflict
	case 429:
		return ErrRateLimited
	}
	return nil
}

// Fragments of Supermarket's api.error_messages locale strings (upstream
// config/locales/en.yml) that identify a reason. Interpolated values are
// left out: the username, the host and the cookbook name and version.
const (
	msgVersionNotUnique     = ") already exists. A cookbook's version number must be unique."
	msgInvalidUsername      = "Could not find user '"
	msgMissingPublicKey     = "Before you can perform knife activities that require authentication"
	msgKeyPairRejected      = "Authentication failed due to an invalid public/private key pair."
	msgInvalidSignedRequest = "Authentication failed due to an invalid signed request."
)

// reason returns the reason sentinel the response's message identifies, or
// nil when it matches none.
func (e *ErrorResponse) reason() error {
	for _, m := range e.Messages {
		switch e.Code {
		case "INVALID_DATA":
			if strings.HasSuffix(m, msgVersionNotUnique) {
				return ErrVersionExists
			}
		case "AUTHENTICATION_FAILED":
			switch {
			case strings.HasPrefix(m, msgInvalidUsername):
				return ErrUnknownUser
			case strings.HasPrefix(m, msgMissingPublicKey):
				return ErrNoPublicKey
			case strings.HasPrefix(m, msgKeyPairRejected):
				return ErrSignatureRejected
			case strings.HasPrefix(m, msgInvalidSignedRequest):
				return ErrInvalidSignedRequest
			}
		}
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
