// Package signing implements the Chef mixlib-authentication signed-header
// protocol used by Supermarket's write endpoints.
//
// We sign with version 1.1 (SHA-1) rather than the modern version 1.3
// (SHA-256). See SignVersion in signer.go for why.
package signing

import (
	"crypto/sha1"
	"encoding/base64"
	"regexp"
	"strings"
)

// ServerAPIVersion is the X-Ops-Server-API-Version the client requests.
// Note: it is NOT part of the v1.0/1.1 canonical request (that field only
// exists in the v1.3 canonicalization); it's sent as a plain header.
const ServerAPIVersion = "1"

// Request is the minimal set of fields needed to sign an HTTP request.
type Request struct {
	Method    string // upper-case HTTP method
	Path      string // request path, will be canonicalized
	Body      []byte // request body, may be nil
	UserID    string // Supermarket username
	Timestamp string // ISO-8601 UTC, e.g. 2024-01-01T00:00:00Z
}

// hashString returns base64(SHA1(s)) — mixlib's digester.hash_string. The
// standard base64 alphabet, no embedded or trailing newlines (a SHA-1
// digest is 20 bytes / 28 base64 chars, so it never wraps).
func hashString(s []byte) string {
	sum := sha1.Sum(s)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// ContentHash returns base64(SHA1(body)) — the X-Ops-Content-Hash value
// for sign version 1.0/1.1.
func ContentHash(body []byte) string {
	return hashString(body)
}

var multiSlash = regexp.MustCompile(`/+`)

// CanonicalPath mirrors mixlib's canonical_path: collapse repeated slashes
// to one and strip a trailing slash (except for the root "/").
func CanonicalPath(p string) string {
	if p == "" {
		return "/"
	}
	p = multiSlash.ReplaceAllString(p, "/")
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

// CanonicalRequest builds the v1.0/1.1 string-to-sign for r. This is the
// exact five-line block mixlib-authentication's canonicalize_request emits
// for proto version 1.1 (joined by "\n", no trailing newline):
//
//	Method:<UPPERCASE METHOD>
//	Hashed Path:<base64(SHA1(canonicalPath))>
//	X-Ops-Content-Hash:<base64(SHA1(body))>
//	X-Ops-Timestamp:<timestamp>
//	X-Ops-UserId:<base64(SHA1(userId))>   # hashed for v1.1
//
// For v1.1 the user id is hashed; the X-Ops-UserId *header* still carries
// the raw user id (see signer.go).
func CanonicalRequest(r Request) string {
	return strings.Join([]string{
		"Method:" + strings.ToUpper(r.Method),
		"Hashed Path:" + hashString([]byte(CanonicalPath(r.Path))),
		"X-Ops-Content-Hash:" + ContentHash(r.Body),
		"X-Ops-Timestamp:" + r.Timestamp,
		"X-Ops-UserId:" + hashString([]byte(r.UserID)),
	}, "\n")
}
