package signing

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"maps"
	"os"
	"os/exec"
	"strconv"
	"testing"
)

// This file is the gem-backed half of the signing guard: it runs our real
// SignHeaders output through mixlib-authentication's SERVER-side verifier
// (testdata/verify.rb) and asserts the request authenticates — the check the
// byte-for-byte signer vectors can't make, because they never involve the
// verifier that actually rejected the v1.3 regression in production.
//
// It is the gold standard but is GATED on Ruby + the mixlib-authentication
// gem being installed; when they're absent it t.Skips with a clear message
// rather than failing, so the default `go test ./...` stays fast and
// dependency-free. The Ruby-free TestSignVersionIsAcceptedBySupermarket guard
// is what always runs.

// requireMixlib skips the test unless ruby and the mixlib-authentication gem
// are both available.
func requireMixlib(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ruby"); err != nil {
		t.Skip("skipping mixlib verifier test: ruby not found on PATH " +
			"(install Ruby + `gem install mixlib-authentication` to run the gold-standard verifier check)")
	}
	probe := exec.Command("ruby", "-e", `require "mixlib/authentication/signatureverification"`)
	if out, err := probe.CombinedOutput(); err != nil {
		t.Skipf("skipping mixlib verifier test: mixlib-authentication gem not available "+
			"(`gem install mixlib-authentication`): %v: %s", err, out)
	}
}

// runVerifyRB pipes doc as JSON into testdata/verify.rb and decodes its JSON
// reply.
func runVerifyRB(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	in, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("ruby", "testdata/verify.rb")
	cmd.Stdin = bytes.NewReader(in)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("verify.rb (op=%v) failed: %v\nstderr: %s", doc["op"], err, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decoding verify.rb output %q: %v", stdout.String(), err)
	}
	return out
}

// privateKeyPEM returns the raw PEM bytes of the shared test key.
func privateKeyPEM(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../testdata/test_key.pem")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// testPublicKeyPEM returns the PKIX public-key PEM for the shared test key —
// the only half mixlib's verifier needs, exactly as the Supermarket server
// would hold only the user's public key.
func testPublicKeyPEM(t *testing.T) string {
	t.Helper()
	key := testKey(t)
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

// hugeSkew lets the verifier accept our fixed test timestamps regardless of
// the wall clock, keeping the test deterministic. The signature, not the
// clock, is what we're checking here.
const hugeSkew = 100 * 365 * 24 * 60 * 60 // ~100 years, in seconds

// supportedVersions mirrors the Supermarket-accepted set the verifier gates on.
var supportedVersions = []any{"1.0", "1.1"}

// TestVerifier_SignHeadersAuthenticates is the positive gold-standard case:
// a request our SignHeaders signed must authenticate under mixlib's
// server-side verifier, gated to the versions the public Supermarket accepts.
func TestVerifier_SignHeadersAuthenticates(t *testing.T) {
	requireMixlib(t)
	key := testKey(t)

	cases := []struct {
		name string
		req  Request
	}{
		{"get_no_body", Request{Method: "GET", Path: "/api/v1/cookbooks", UserID: "tester", Timestamp: "2024-01-01T00:00:00Z"}},
		{"post_json_body", Request{Method: "POST", Path: "/api/v1/cookbooks", Body: []byte(`{"a":1}`), UserID: "alice", Timestamp: "2024-01-01T00:00:00Z"}},
		{"delete_messy_path", Request{Method: "DELETE", Path: "//api/v1//cookbooks/apache2/", UserID: "bob", Timestamp: "2024-12-31T23:59:59Z"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := SignHeaders(tc.req, key)
			if err != nil {
				t.Fatal(err)
			}
			out := runVerifyRB(t, map[string]any{
				"op":               "verify",
				"method":           tc.req.Method,
				"path":             tc.req.Path,
				"host":             "supermarket.chef.io",
				"body_b64":         base64.StdEncoding.EncodeToString(tc.req.Body),
				"public_key_pem":   testPublicKeyPEM(t),
				"allowed_versions": supportedVersions,
				"time_skew":        hugeSkew,
				"headers":          headerMap(h),
			})
			if out["authenticated"] != true {
				t.Fatalf("mixlib verifier did NOT authenticate our signed request: %+v", out)
			}
		})
	}
}

// TestVerifier_RejectsVersion13 is the negative case proving the guard: a
// genuine, cryptographically valid version=1.3 request — the exact thing the
// shipped bug produced — is rejected by the verifier gated to the
// Supermarket-supported versions. To prove the rejection is the version gate
// and not a malformed signature, the same request authenticates once 1.3 is
// added to the allowed set.
func TestVerifier_RejectsVersion13(t *testing.T) {
	requireMixlib(t)

	// Mint a real 1.3 (SHA-256) request via mixlib's own signer — our Go
	// signer deliberately can't produce one.
	signed := runVerifyRB(t, map[string]any{
		"op":              "sign",
		"method":          "POST",
		"path":            "/api/v1/cookbooks",
		"body_b64":        base64.StdEncoding.EncodeToString([]byte(`{"a":1}`)),
		"user_id":         "tester",
		"timestamp":       "2024-01-01T00:00:00Z",
		"proto_version":   "1.3",
		"private_key_pem": privateKeyPEM(t),
	})
	hdrs, ok := signed["headers"].(map[string]any)
	if !ok {
		t.Fatalf("sign op did not return headers: %+v", signed)
	}
	if got := hdrs["X-Ops-Sign"]; got != "algorithm=sha256;version=1.3;" {
		t.Fatalf("expected a v1.3 signed request, got X-Ops-Sign=%v", got)
	}

	base := map[string]any{
		"op":             "verify",
		"method":         "POST",
		"path":           "/api/v1/cookbooks",
		"host":           "supermarket.chef.io",
		"body_b64":       base64.StdEncoding.EncodeToString([]byte(`{"a":1}`)),
		"public_key_pem": testPublicKeyPEM(t),
		"time_skew":      hugeSkew,
		"headers":        hdrs,
	}

	// Gated to the Supermarket-supported set: REJECTED.
	rejected := runVerifyRB(t, withAllowed(base, supportedVersions))
	if rejected["authenticated"] != false {
		t.Fatalf("v1.3 request should be rejected under versions %v, got %+v", supportedVersions, rejected)
	}
	if rejected["version_allowed"] != false {
		t.Fatalf("expected the version gate to flag 1.3 as disallowed, got %+v", rejected)
	}

	// With 1.3 explicitly allowed the very same request authenticates,
	// proving the rejection above was the version gate, not a bad signature.
	allowed := runVerifyRB(t, withAllowed(base, []any{"1.0", "1.1", "1.3"}))
	if allowed["authenticated"] != true {
		t.Fatalf("v1.3 request should authenticate once 1.3 is allowed (proves the signature is valid): %+v", allowed)
	}
}

func withAllowed(base map[string]any, allowed []any) map[string]any {
	out := make(map[string]any, len(base)+1)
	maps.Copy(out, base)
	out["allowed_versions"] = allowed
	return out
}

// headerMap flattens an http.Header into a name->value map for the JSON the
// Ruby verifier reads (every signed header is single-valued).
func headerMap(h interface{ Get(string) string }) map[string]any {
	names := []string{
		"X-Ops-Sign", "X-Ops-Userid", "X-Ops-Timestamp", "X-Ops-Content-Hash",
		"X-Ops-Server-Api-Version",
	}
	m := map[string]any{}
	for _, n := range names {
		if v := h.Get(n); v != "" {
			m[n] = v
		}
	}
	// Authorization chunks are numbered from 1.
	for i := 1; ; i++ {
		k := "X-Ops-Authorization-" + strconv.Itoa(i)
		v := h.Get(k)
		if v == "" {
			break
		}
		m[k] = v
	}
	return m
}
