package supermarket

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTLSHealthServer starts an HTTPS server with a self-signed certificate
// that no system trust store contains — the situation of a private
// Supermarket behind an internal CA.
func newTLSHealthServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestPrivateCAIsRejectedByDefault is the control for the test below: without
// the CA, verification must fail, or WithRootCAs proves nothing.
func TestPrivateCAIsRejectedByDefault(t *testing.T) {
	srv := newTLSHealthServer(t)
	c, err := NewClient(Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = c.Health.Status(context.Background())
	if _, ok := errors.AsType[x509.UnknownAuthorityError](err); !ok {
		t.Fatalf("Status against an untrusted CA: err = %v, want x509.UnknownAuthorityError", err)
	}
}

// TestWithRootCAsTrustsPrivateCA — WithRootCAs is the way to reach a private
// Supermarket without disabling verification: the server's certificate is
// still checked, just against the caller's pool.
func TestWithRootCAsTrustsPrivateCA(t *testing.T) {
	srv := newTLSHealthServer(t)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	c, err := NewClient(Config{BaseURL: srv.URL}, WithRootCAs(pool))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Health.Status(context.Background()); err != nil {
		t.Fatalf("Status with the CA in the pool: %v", err)
	}
	// The streaming client is derived separately; it must trust the pool too,
	// or Download and Universe.GetStream fail where every other call works.
	body, _, err := c.Universe.GetStream(context.Background())
	if err != nil && strings.Contains(err.Error(), "certificate") {
		t.Fatalf("GetStream ignored the root CAs: %v", err)
	}
	if body != nil {
		_ = body.Close()
	}
}

// TestWithRootCAsStillVerifies — a pool that lacks the server's CA must fail.
// This is what separates the option from InsecureSkipVerify. (An empty pool
// rather than a second test server's cert: every httptest TLS server shares
// one built-in certificate, so that pool would trust this server too.)
func TestWithRootCAsStillVerifies(t *testing.T) {
	srv := newTLSHealthServer(t)

	c, err := NewClient(Config{BaseURL: srv.URL}, WithRootCAs(x509.NewCertPool()))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Health.Status(context.Background()); err == nil {
		t.Fatal("Status succeeded against a CA missing from the pool; verification is off")
	}
}

// TestWithRootCAsRejectsCustomRoundTripper — like WithSkipTLSVerify, the pool
// lives on *http.Transport, so a custom RoundTripper cannot honour it. Silently
// ignoring the pool would leave the caller believing the CA is trusted.
func TestWithRootCAsRejectsCustomRoundTripper(t *testing.T) {
	_, err := NewClient(Config{BaseURL: "https://x.test"},
		WithHTTPClient(&http.Client{Transport: &markerRoundTripper{}}),
		WithRootCAs(x509.NewCertPool()))
	if err == nil {
		t.Fatal("NewClient accepted WithRootCAs alongside a custom RoundTripper; the pool would be ignored")
	}
	if !strings.Contains(err.Error(), "WithRootCAs") {
		t.Errorf("error = %q, want it to name the offending option", err)
	}
}

// TestWithRootCAsNilIsIgnored — nil means "no override", matching
// WithHTTPClient(nil); it must not swap in an empty pool that trusts nothing.
func TestWithRootCAsNilIsIgnored(t *testing.T) {
	c, err := NewClient(Config{BaseURL: "https://x.test"}, WithRootCAs(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.httpClient.Transport != nil {
		t.Errorf("transport = %T, want the default client left untouched", c.httpClient.Transport)
	}
}
