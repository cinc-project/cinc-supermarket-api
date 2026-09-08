package supermarket

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestWithHTTPClientUsesGivenClient(t *testing.T) {
	custom := &http.Client{Timeout: 7 * time.Second}
	c, err := NewClient(Config{BaseURL: "https://x.test"}, WithHTTPClient(custom))
	if err != nil {
		t.Fatal(err)
	}
	if c.httpClient != custom {
		t.Error("WithHTTPClient did not install the provided *http.Client")
	}
}

func TestWithHTTPClientIgnoresNil(t *testing.T) {
	c, err := NewClient(Config{BaseURL: "https://x.test"}, WithHTTPClient(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.httpClient == nil {
		t.Error("nil *http.Client should be silently ignored, not installed")
	}
}

func TestWithMaxRetriesIgnoresNegative(t *testing.T) {
	c, err := NewClient(Config{BaseURL: "https://x.test"}, WithMaxRetries(-3))
	if err != nil {
		t.Fatal(err)
	}
	if c.opts.maxRetries == -3 {
		t.Errorf("negative MaxRetries should be ignored, got %d", c.opts.maxRetries)
	}
}

func TestWithUserAgentOverridesDefault(t *testing.T) {
	var got string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewClient(Config{BaseURL: srv.URL}, WithUserAgent("my-tool/1.0"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Health.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got != "my-tool/1.0" {
		t.Errorf("User-Agent = %q, want my-tool/1.0", got)
	}
}

func TestWithSkipTLSVerifyPreservesCustomClient(t *testing.T) {
	// When a caller supplies their own *http.Client and also asks to
	// skip TLS verification, the custom transport settings and client
	// fields must survive — only InsecureSkipVerify should be flipped on.
	custom := &http.Client{
		Timeout:       42 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport:     &http.Transport{MaxIdleConns: 99},
	}
	c, err := NewClient(Config{BaseURL: "https://x.test"},
		WithHTTPClient(custom), WithSkipTLSVerify(true))
	if err != nil {
		t.Fatal(err)
	}
	if c.httpClient.Timeout != 42*time.Second {
		t.Errorf("Timeout = %v, want 42s (custom client dropped)", c.httpClient.Timeout)
	}
	if c.httpClient.CheckRedirect == nil {
		t.Error("CheckRedirect was dropped from the custom client")
	}
	tr, ok := c.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", c.httpClient.Transport)
	}
	if tr.MaxIdleConns != 99 {
		t.Errorf("MaxIdleConns = %d, want 99 (custom transport settings dropped)", tr.MaxIdleConns)
	}
	if tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify = false, want true")
	}
}

func TestWithSkipTLSVerifyEnablesInsecureTransport(t *testing.T) {
	c, err := NewClient(Config{BaseURL: "https://x.test"}, WithSkipTLSVerify(true))
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := c.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", c.httpClient.Transport)
	}
	if tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
		t.Errorf("InsecureSkipVerify = false, want true")
	}
}

// markerRoundTripper records whether it was ever asked to carry a request.
type markerRoundTripper struct{ used atomic.Bool }

func (m *markerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	m.used.Store(true)
	return http.DefaultTransport.RoundTrip(r)
}

// TestWithSkipTLSVerifyRejectsCustomRoundTripper — InsecureSkipVerify lives on
// *http.Transport's TLS config, so there is no way to flip it on an arbitrary
// RoundTripper. The old code fell back to http.DefaultTransport, silently
// discarding the caller's transport: an oauth2 wrapper, an OpenTelemetry
// round-tripper, or a VCR recorder would simply stop being used, with no error
// anywhere. Refusing the combination is the honest answer.
func TestWithSkipTLSVerifyRejectsCustomRoundTripper(t *testing.T) {
	rt := &markerRoundTripper{}
	_, err := NewClient(Config{BaseURL: "https://x.test"},
		WithHTTPClient(&http.Client{Transport: rt}),
		WithSkipTLSVerify(true))
	if err == nil {
		t.Fatal("NewClient accepted WithSkipTLSVerify alongside a custom RoundTripper; " +
			"the transport would be silently discarded")
	}
	if !strings.Contains(err.Error(), "WithSkipTLSVerify") {
		t.Errorf("error = %q, want it to name the offending option", err)
	}
}

// TestCustomRoundTripperIsUsedWithoutSkipTLSVerify — the rejection above must
// be scoped to the conflicting combination. On its own a custom RoundTripper
// has to keep carrying every request.
func TestCustomRoundTripperIsUsedWithoutSkipTLSVerify(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	rt := &markerRoundTripper{}
	c, err := NewClient(Config{BaseURL: srv.URL},
		WithHTTPClient(&http.Client{Transport: rt}))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Health.Status(context.Background()); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !rt.used.Load() {
		t.Error("the custom RoundTripper never carried the request")
	}
}
