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

// TestTransportSetsCommonHeaders covers the headers every request gets
// regardless of resource: User-Agent (always), Accept (always), and
// Content-Type only when there's a body to send.
func TestTransportSetsCommonHeaders(t *testing.T) {
	var (
		gotUA          string
		gotAccept      string
		gotContentType string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks", func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		_, _ = io.WriteString(w, `{"total":0,"start":0,"items":[]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	if _, _, err := c.Cookbooks.List(context.Background(), ListOptions{}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if gotUA != "cinc-supermarket-go" {
		t.Errorf("User-Agent = %q, want default cinc-supermarket-go", gotUA)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
	if gotContentType != "" {
		t.Errorf("Content-Type on a GET (no body) = %q, want empty", gotContentType)
	}
}

// TestTransportRetriesGETOn5xx confirms idempotent GETs are retried up
// to MaxRetries times against a transient 500.
func TestTransportRetriesGETOn5xx(t *testing.T) {
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks/apache2", func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, `{"name":"apache2"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	if _, _, err := c.Cookbooks.Get(context.Background(), "apache2"); err != nil {
		t.Fatalf("Get after retry: %v", err)
	}
	if got := hits.Load(); got != 3 {
		t.Errorf("server hits = %d, want 3 (2 transient + 1 success)", got)
	}
}

// TestTransportDoesNotRetryGETOn4xx — a 4xx is a definitive answer from
// the server (bad request, not found, forbidden). Retrying it wastes
// round-trips and can amplify load for, e.g., a not-found lookup.
func TestTransportDoesNotRetryGETOn4xx(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var hits atomic.Int32
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v1/cookbooks/nope", func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error_code":"NOT_FOUND","error_messages":["x"]}`)
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)
			c := newTestClient(t, srv, false)

			if _, _, err := c.Cookbooks.Get(context.Background(), "nope"); err == nil {
				t.Fatalf("expected an error from a %d GET", status)
			}
			if got := hits.Load(); got != 1 {
				t.Errorf("server hits = %d, want 1 (4xx must not be retried)", got)
			}
		})
	}
}

// TestTransportRetriesGETOnTransportError confirms a genuine transport
// failure (connection refused — no HTTP response at all) is retried on
// a GET, distinct from the HTTP-status path.
func TestTransportRetriesGETOnTransportError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close() // nothing is listening now → dial fails

	c, err := NewClient(Config{BaseURL: addr}, WithMaxRetries(2))
	if err != nil {
		t.Fatal(err)
	}
	// A failing dial returns an error; the value here is that it does so
	// after exhausting retries rather than hanging or panicking.
	if _, _, err := c.Cookbooks.List(context.Background(), ListOptions{}); err == nil {
		t.Error("expected a transport error against a closed server")
	}
}

// TestTransportDoesNotRetryNonGET makes sure POSTs and DELETEs never
// silently re-execute on 5xx — replays of writes are dangerous.
func TestTransportDoesNotRetryNonGET(t *testing.T) {
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks/apache2", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q", r.Method)
		}
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, true)

	if _, _, err := c.Cookbooks.Delete(context.Background(), "apache2"); err == nil {
		t.Error("expected an error from a 500-returning DELETE")
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("DELETE hits = %d, want 1 (no retries on writes)", got)
	}
}

// TestTransportRespectsMaxRetriesZero turns retries off entirely.
func TestTransportRespectsMaxRetriesZero(t *testing.T) {
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks", func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewClient(Config{BaseURL: srv.URL}, WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Cookbooks.List(context.Background(), ListOptions{}); err == nil {
		t.Error("expected an error from 502-returning GET with retries disabled")
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("hits = %d, want 1 with MaxRetries=0", got)
	}
}

// TestTransportDoesNotRetryOnContextCancel — a context cancel is
// terminal; retrying would burn cycles after the caller has already
// given up.
func TestTransportDoesNotRetryOnContextCancel(t *testing.T) {
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		// Wait long enough for the caller's context to fire.
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := c.Cookbooks.List(ctx, ListOptions{}); err == nil {
		t.Error("expected a context-deadline error")
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("hits = %d, want 1 (no retry on context cancel)", got)
	}
}

// TestTransportPreservesQueryStringOnTheWire — the signed canonical
// request strips the query string before signing (signing.CanonicalPath
// is exercised by the signing package), but the request that hits the
// server still carries the query. This guards against accidentally
// signing/sending the unstripped path.
func TestTransportPreservesQueryStringOnTheWire(t *testing.T) {
	var (
		gotPath  string
		gotQuery string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"total":0,"start":0,"items":[]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	if _, _, err := c.Cookbooks.List(context.Background(), ListOptions{Start: 5, Items: 2}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if gotPath != "/api/v1/cookbooks" {
		t.Errorf("server saw path = %q, want /api/v1/cookbooks", gotPath)
	}
	if !strings.Contains(gotQuery, "start=5") || !strings.Contains(gotQuery, "items=2") {
		t.Errorf("server saw query = %q, want start=5 and items=2", gotQuery)
	}
}

// TestStreamReturnsErrorWithoutBody — non-2xx on the stream path closes
// the body and returns the error envelope, not an open ReadCloser.
func TestStreamReturnsErrorWithoutBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/universe", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error_messages":["maintenance"]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	body, _, err := c.Universe.GetStream(context.Background())
	if err == nil {
		t.Fatal("expected an error from a 503 on the stream path")
	}
	if body != nil {
		t.Error("non-nil body returned alongside an error")
	}
}

// TestStreamIsNotTruncatedByClientTimeout — http.Client.Timeout spans the
// entire body read, not just the connect/header phase, so a shared 60s client
// deadline silently truncates the two APIs that exist precisely to move large
// payloads: /universe (tens of megabytes) and cookbook tarball downloads.
// Streaming responses must be bounded by the caller's context and by
// transport-level phase timeouts instead.
func TestStreamIsNotTruncatedByClientTimeout(t *testing.T) {
	const (
		chunks   = 5
		chunk    = "chunk"
		perChunk = 60 * time.Millisecond
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/universe", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		for range chunks {
			_, _ = io.WriteString(w, chunk)
			w.(http.Flusher).Flush()
			time.Sleep(perChunk)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// The body takes ~300ms to arrive; the client deadline is well under that.
	c, err := NewClient(Config{BaseURL: srv.URL},
		WithHTTPClient(&http.Client{Timeout: 150 * time.Millisecond}))
	if err != nil {
		t.Fatal(err)
	}
	body, _, err := c.Universe.GetStream(context.Background())
	if err != nil {
		t.Fatalf("GetStream: %v", err)
	}
	defer body.Close()

	n, err := io.Copy(io.Discard, body)
	if err != nil {
		t.Fatalf("reading stream: %v", err)
	}
	if want := int64(chunks * len(chunk)); n != want {
		t.Errorf("streamed %d bytes, want %d (Client.Timeout truncated the body)", n, want)
	}
}

// TestStreamStillHonorsContextCancellation — dropping the total-transaction
// deadline must not leave streams unbounded; the caller's context is the
// remaining brake and has to keep working.
func TestStreamStillHonorsContextCancellation(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/universe", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	body, _, err := c.Universe.GetStream(ctx)
	if err != nil {
		t.Fatalf("GetStream: %v", err)
	}
	defer body.Close()
	if _, err := io.Copy(io.Discard, body); err == nil {
		t.Error("expected the read to fail once the context deadline passed")
	}
}

// collectOpsHeaders records the X-Ops-* header names a handler received.
func collectOpsHeaders(r *http.Request) []string {
	var got []string
	for k := range r.Header {
		if strings.HasPrefix(k, "X-Ops-") {
			got = append(got, k)
		}
	}
	return got
}

// TestSignedHeadersAreStrippedOnCrossHostRedirect — Go strips Authorization,
// Cookie, and WWW-Authenticate when a redirect crosses to another host, but it
// knows nothing about Chef's X-Ops-* block, which is credential material of
// the same kind. Forwarding it hands a username and a full RSA signature to a
// host the caller never addressed.
func TestSignedHeadersAreStrippedOnCrossHostRedirect(t *testing.T) {
	var leaked []string
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = collectOpsHeaders(r)
		_, _ = io.WriteString(w, `{"name":"apache2"}`)
	}))
	t.Cleanup(dest.Close)

	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dest.URL+"/api/v1/cookbooks/apache2", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(src.Close)

	c := newTestClient(t, src, true)
	if _, _, err := c.Cookbooks.Delete(context.Background(), "apache2"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(leaked) != 0 {
		t.Errorf("signed headers leaked to a different host: %v", leaked)
	}
}

// TestSignedHeadersSurviveSameHostRedirect — the guard must be scoped to a
// host change. A Supermarket that redirects within its own origin still needs
// the signature, or every signed write behind such a redirect would 401.
func TestSignedHeadersSurviveSameHostRedirect(t *testing.T) {
	var got []string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks/apache2", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api/v1/cookbooks/apache2/", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/api/v1/cookbooks/apache2/", func(w http.ResponseWriter, r *http.Request) {
		got = collectOpsHeaders(r)
		_, _ = io.WriteString(w, `{"name":"apache2"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, true)
	if _, _, err := c.Cookbooks.Delete(context.Background(), "apache2"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(got) == 0 {
		t.Error("signed headers were dropped on a same-host redirect")
	}
}

// TestSignedClientStillLimitsRedirects — installing a CheckRedirect func
// disables net/http's built-in 10-redirect cap, so the replacement has to
// reimpose it or a redirect loop hangs the caller.
func TestSignedClientStillLimitsRedirects(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "/api/v1/cookbooks/loop", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, true)
	if _, _, err := c.Cookbooks.Delete(context.Background(), "loop"); err == nil {
		t.Fatal("expected an error from an endless redirect loop")
	}
	if n := hits.Load(); n > 15 {
		t.Errorf("followed %d redirects, want the request capped near 10", n)
	}
}

// TestStreamSendsAcceptHeader — every request through doJSON advertises
// Accept: application/json, but the streaming path sent no Accept at all. The
// two streaming callers want different things, so the header has to be chosen
// per caller rather than hardcoded.
func TestStreamSendsAcceptHeader(t *testing.T) {
	var universeAccept, downloadAccept string
	mux := http.NewServeMux()
	mux.HandleFunc("/universe", func(w http.ResponseWriter, r *http.Request) {
		universeAccept = r.Header.Get("Accept")
		_, _ = io.WriteString(w, `{}`)
	})
	mux.HandleFunc("/api/v1/cookbooks/nginx/versions/1_0_0/download", func(w http.ResponseWriter, r *http.Request) {
		downloadAccept = r.Header.Get("Accept")
		_, _ = io.WriteString(w, "tarball")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)
	ctx := context.Background()

	body, _, err := c.Universe.GetStream(ctx)
	if err != nil {
		t.Fatalf("GetStream: %v", err)
	}
	_ = body.Close()
	if !strings.Contains(universeAccept, "application/json") {
		t.Errorf("GetStream Accept = %q, want it to include application/json", universeAccept)
	}

	body, _, err = c.Cookbooks.Download(ctx, "nginx", "1.0.0")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	_ = body.Close()
	if downloadAccept == "" {
		t.Error("Download sent no Accept header")
	}
	if strings.Contains(downloadAccept, "application/json") {
		t.Errorf("Download Accept = %q, want a tarball type, not JSON", downloadAccept)
	}
}

// TestStreamResponseExposesAnOpenBody pins what a streaming Response actually
// carries. The Response doc comment claimed the body "has already been
// consumed and closed by the time the caller sees the Response", which is true
// for doJSON and false here — the whole point of the streaming path is that
// the caller reads and closes it.
func TestStreamResponseExposesAnOpenBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/universe", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"a":{}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	body, resp, err := c.Universe.GetStream(context.Background())
	if err != nil {
		t.Fatalf("GetStream: %v", err)
	}
	t.Cleanup(func() { _ = body.Close() })

	if resp.HTTPResponse.Body != body {
		t.Error("resp.HTTPResponse.Body is not the ReadCloser handed to the caller")
	}
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("the body was not readable: %v", err)
	}
	if string(got) != `{"a":{}}` {
		t.Errorf("read %q, want the full document", got)
	}
}
