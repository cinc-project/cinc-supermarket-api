package supermarket

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestResponsePromotesHTTPResponseFields pins that Response embeds
// *http.Response, so a caller reads the status and headers straight off the
// returned *Response for both a decoding call and a streaming call.
func TestResponsePromotesHTTPResponseFields(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks/apache2", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-Id", "decode-1")
		_, _ = io.WriteString(w, `{"name":"apache2"}`)
	})
	mux.HandleFunc("/universe", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-Id", "stream-1")
		_, _ = io.WriteString(w, `{"a":{}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	_, resp, err := c.Cookbooks.Get(context.Background(), "apache2")
	if err != nil {
		t.Fatalf("Cookbooks.Get: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("decoding call: resp.StatusCode = %d, want 200; the promoted status is not the server's", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Request-Id"); got != "decode-1" {
		t.Errorf("decoding call: resp.Header X-Request-Id = %q, want %q; response headers are not reachable on Response", got, "decode-1")
	}

	body, resp, err := c.Universe.GetStream(context.Background())
	if err != nil {
		t.Fatalf("Universe.GetStream: %v", err)
	}
	t.Cleanup(func() { _ = body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Errorf("streaming call: resp.StatusCode = %d, want 200; the promoted status is not the server's", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Request-Id"); got != "stream-1" {
		t.Errorf("streaming call: resp.Header X-Request-Id = %q, want %q; response headers are not reachable on Response", got, "stream-1")
	}
	if resp.Body != body {
		t.Error("streaming call: resp.Body is not the ReadCloser handed to the caller")
	}
}

// TestRetriableToleratesEmptyResponse: with *http.Response embedded, reading
// StatusCode off a Response whose embedded pointer is nil would panic. The
// retry loop must treat such a value like no response at all.
func TestRetriableToleratesEmptyResponse(t *testing.T) {
	if retriable(&Response{}, nil) {
		t.Error("retriable(&Response{}, nil) = true; an empty Response with no error is not a retriable failure")
	}
}
