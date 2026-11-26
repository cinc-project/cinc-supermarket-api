package signing

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
)

// SignVersion is the mixlib-authentication signing protocol version we use.
//
// We sign with mixlib-authentication version 1.1 (SHA-1) because signing with
// 1.3 (SHA-256), the scheme this library originally used, got a 401 on every
// share against supermarket.chef.io in production. The cause is unknown:
// upstream mixlib-authentication's verifier does handle 1.3. Only move back to
// 1.3 after a 1.3 share has been shown to succeed against the target server.
const SignVersion = "1.1"

// signHeaderValue is the literal X-Ops-Sign header value. It mirrors
// mixlib-authentication byte-for-byte, including the trailing semicolon, so
// the server parses it exactly as it does a real knife request.
const signHeaderValue = "algorithm=sha1;version=" + SignVersion + ";"

// chunk60 splits s into 60-character pieces (the X-Ops-Authorization-N width).
func chunk60(s string) []string {
	var out []string
	for len(s) > 60 {
		out = append(out, s[:60])
		s = s[60:]
	}
	if len(s) > 0 {
		out = append(out, s)
	}
	return out
}

// SignHeaders signs r with key and returns the headers to add to the request.
//
// Version 1.0/1.1 signs the canonical-request STRING with a raw RSA
// private-key operation (Ruby's private_encrypt). In Go that is exactly
// rsa.SignPKCS1v15 with crypto.Hash(0): PKCS#1 v1.5 padding over the raw
// message with NO DigestInfo prefix. The canonical request (~200 bytes for
// v1.1) must fit the modulus: PKCS#1 v1.5 needs len(msg) <= keySize/8 - 11,
// which a 2048-bit key (the Chef standard) easily satisfies.
func SignHeaders(r Request, key *rsa.PrivateKey) (http.Header, error) {
	canonical := []byte(CanonicalRequest(r))
	if max := key.Size() - 11; len(canonical) > max {
		return nil, fmt.Errorf("signing: RSA key too small: canonical request is %d bytes but a %d-bit key can sign at most %d; use a 2048-bit (or larger) key",
			len(canonical), key.Size()*8, max)
	}
	// crypto.Hash(0) => sign the raw message (no DigestInfo), matching
	// mixlib's private_encrypt of the canonical-request string.
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.Hash(0), canonical)
	if err != nil {
		return nil, fmt.Errorf("signing: %w", err)
	}
	h := http.Header{}
	h.Set("X-Ops-Sign", signHeaderValue)
	h.Set("X-Ops-UserId", r.UserID) // header stays raw; canonical request hashes it for v1.1
	h.Set("X-Ops-Timestamp", r.Timestamp)
	h.Set("X-Ops-Content-Hash", ContentHash(r.Body))
	h.Set("X-Ops-Server-API-Version", ServerAPIVersion)
	for i, chunk := range chunk60(base64.StdEncoding.EncodeToString(sig)) {
		h.Set("X-Ops-Authorization-"+strconv.Itoa(i+1), chunk)
	}
	return h, nil
}
