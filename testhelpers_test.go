package supermarket

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// testRSAKey generates a small RSA key suitable for tests that need
// signed requests. 2048-bit so signatures are realistic.
func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// newTestClient builds a Client pointed at srv. When signed is true the
// client carries credentials and can hit write endpoints.
//
// Retry backoff defaults to zero here so the suite stays fast; pass an
// explicit WithRetryBackoff in opts to exercise the waiting itself.
func newTestClient(t *testing.T, srv *httptest.Server, signed bool, opts ...Option) *Client {
	t.Helper()
	cfg := Config{BaseURL: srv.URL}
	if signed {
		cfg.Username = "tester"
		cfg.Key = testRSAKey(t)
	}
	c, err := NewClient(cfg, append([]Option{WithRetryBackoff(0)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// fixture reads a response body from testdata/contract/: recorded from
// production, or (under derived/) built from upstream Supermarket's views.
// Serving these instead of hand-written JSON keeps a mock from drifting into
// a shape the server never sends.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "contract", name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return data
}

// serveFixture answers every request to path with status and the named
// fixture, recording the last request so a test can inspect what was sent.
func serveFixture(t *testing.T, path string, status int, name string) (*httptest.Server, *http.Request) {
	t.Helper()
	body := fixture(t, name)
	last := new(http.Request)
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		*last = *r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, last
}

// newServer starts an httptest server for mux and closes it with the test.
func newServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}
