package supermarket

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestUniverseGetDecodesNestedMap(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/universe", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"apache2": {
				"1.0.0": {"location_path":"u1","location_type":"supermarket","dependencies":{"openssl":">= 0.0.0"}},
				"2.0.0": {"location_path":"u2","location_type":"supermarket","dependencies":{}}
			}
		}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	uni, _, err := c.Universe.Get(context.Background())
	if err != nil {
		t.Fatalf("Universe.Get: %v", err)
	}
	apache := uni["apache2"]
	if apache["1.0.0"].Dependencies["openssl"] != ">= 0.0.0" {
		t.Errorf("uni = %+v", uni)
	}
	if apache["2.0.0"].LocationPath != "u2" {
		t.Errorf("uni[apache2][2.0.0] = %+v", apache["2.0.0"])
	}
}

func TestUniverseGetHandlesEmptyGraph(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/universe", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	uni, _, err := c.Universe.Get(context.Background())
	if err != nil {
		t.Fatalf("Universe.Get: %v", err)
	}
	if len(uni) != 0 {
		t.Errorf("expected empty universe, got %v", uni)
	}
}

func TestUniverseGetStreamLetsCallerDecodeIncrementally(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/universe", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"apache2":{"1.0.0":{"location_path":"u","location_type":"supermarket","dependencies":{}}}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	body, _, err := c.Universe.GetStream(context.Background())
	if err != nil {
		t.Fatalf("Universe.GetStream: %v", err)
	}
	defer body.Close()
	var uni Universe
	if err := json.NewDecoder(body).Decode(&uni); err != nil {
		t.Fatal(err)
	}
	if _, ok := uni["apache2"]["1.0.0"]; !ok {
		t.Errorf("decoded universe = %+v", uni)
	}
}

// closeRecorder wraps a response body and records whether Close was called.
type closeRecorder struct {
	io.ReadCloser
	closed *atomic.Bool
}

func (c closeRecorder) Close() error {
	c.closed.Store(true)
	return c.ReadCloser.Close()
}

// closeTrackingTransport wraps every response body in a closeRecorder so a
// test can observe, from the client side, whether the body was released.
type closeTrackingTransport struct{ closed atomic.Bool }

func (t *closeTrackingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	resp.Body = closeRecorder{ReadCloser: resp.Body, closed: &t.closed}
	return resp, nil
}

// universeServer serves body at /universe with the given status.
func universeServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/universe", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// collectEntries drains it, returning every entry and every error yielded.
func collectEntries(it func(func(UniverseEntry, error) bool)) ([]UniverseEntry, []error) {
	var entries []UniverseEntry
	var errs []error
	for e, err := range it {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		entries = append(entries, e)
	}
	return entries, errs
}

const twoCookbookUniverse = `{
	"apache2": {
		"1.0.0": {"location_path":"u1","location_type":"supermarket","dependencies":{"openssl":">= 0.0.0"}},
		"2.0.0": {"location_path":"u2","location_type":"supermarket","dependencies":{}}
	},
	"nginx": {
		"12.3.1": {"location_path":"u3","location_type":"supermarket","dependencies":{"apt":">= 2.2","yum":"~> 3.0"}}
	}
}`

func TestUniverseEntriesYieldsEveryVersion(t *testing.T) {
	srv := universeServer(t, http.StatusOK, twoCookbookUniverse)
	c := newTestClient(t, srv, false)

	entries, errs := collectEntries(c.Universe.Entries(context.Background()))
	if len(errs) != 0 {
		t.Fatalf("Entries yielded errors on a well-formed universe: %v", errs)
	}
	got := map[string]UniverseEntry{}
	for _, e := range entries {
		got[e.Cookbook+"@"+e.Version] = e
	}
	if len(entries) != 3 || len(got) != 3 {
		t.Fatalf("got %d entries (%d distinct), want 3; a version was dropped or duplicated: %+v",
			len(entries), len(got), entries)
	}
	a1 := got["apache2@1.0.0"]
	if a1.LocationPath != "u1" || a1.LocationType != "supermarket" || a1.Dependencies["openssl"] != ">= 0.0.0" {
		t.Errorf("apache2@1.0.0 = %+v; the version value was not decoded into the embedded UniverseVersion", a1)
	}
	if a2 := got["apache2@2.0.0"]; a2.LocationPath != "u2" || len(a2.Dependencies) != 0 {
		t.Errorf("apache2@2.0.0 = %+v; fields leaked from the previous version", a2)
	}
	n := got["nginx@12.3.1"]
	if n.Cookbook != "nginx" || n.Dependencies["apt"] != ">= 2.2" || n.Dependencies["yum"] != "~> 3.0" || len(n.Dependencies) != 2 {
		t.Errorf("nginx@12.3.1 = %+v; entries are attributed to the wrong cookbook or lose dependencies", n)
	}
}

func TestUniverseEntriesEmptyUniverseYieldsNothing(t *testing.T) {
	srv := universeServer(t, http.StatusOK, `{}`)
	c := newTestClient(t, srv, false)

	entries, errs := collectEntries(c.Universe.Entries(context.Background()))
	if len(entries) != 0 || len(errs) != 0 {
		t.Errorf("empty universe yielded entries=%v errs=%v; want neither", entries, errs)
	}
}

func TestUniverseEntriesClosesBody(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		stop  bool // break after the first entry
		isErr bool
	}{
		{name: "full iteration", body: twoCookbookUniverse},
		{name: "early break", body: twoCookbookUniverse, stop: true},
		{name: "malformed", body: `{"apache2": [1, 2]}`, isErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := universeServer(t, http.StatusOK, tc.body)
			tr := &closeTrackingTransport{}
			c := newTestClient(t, srv, false, WithHTTPClient(&http.Client{Transport: tr}))

			var sawErr bool
			for _, err := range c.Universe.Entries(context.Background()) {
				if err != nil {
					sawErr = true
				}
				if tc.stop {
					break
				}
			}
			if sawErr != tc.isErr {
				t.Fatalf("sawErr = %v, want %v", sawErr, tc.isErr)
			}
			if !tr.closed.Load() {
				t.Error("response body was not closed; every caller of Entries would leak a connection")
			}
		})
	}
}

func TestUniverseEntriesErrorStatus(t *testing.T) {
	srv := universeServer(t, http.StatusBadRequest,
		`{"error_messages":["Resource not found"],"error_code":"NOT_FOUND"}`)
	c := newTestClient(t, srv, false)

	entries, errs := collectEntries(c.Universe.Entries(context.Background()))
	if len(entries) != 0 {
		t.Errorf("error response yielded entries %v", entries)
	}
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want exactly 1; iteration must stop after the first error: %v", len(errs), errs)
	}
	if !errors.Is(errs[0], ErrNotFound) {
		t.Errorf("err = %v; a NOT_FOUND body must satisfy errors.Is(err, ErrNotFound) or callers cannot classify it", errs[0])
	}
}

func TestUniverseEntriesBadDocumentErrors(t *testing.T) {
	cases := map[string]string{
		"top-level array":          `[]`,
		"cookbook not an object":   `{"apache2": 5}`,
		"version not an object":    `{"apache2": {"1.0.0": [1]}}`,
		"bad field type":           `{"apache2": {"1.0.0": {"dependencies": "openssl"}}}`,
		"syntax error":             `{"apache2": {"1.0.0": {}},,}`,
		"trailing garbage":         `{"apache2": {"1.0.0": {}}} x`,
		"empty body":               ``,
		"truncated after cookbook": `{"apache2": `,
		"truncated mid version":    `{"apache2": {"1.0.0": {"location_path":"u1"`,
		"truncated between":        `{"apache2": {"1.0.0": {}},`,
		"missing final brace":      `{"apache2": {"1.0.0": {}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			srv := universeServer(t, http.StatusOK, body)
			c := newTestClient(t, srv, false)

			_, errs := collectEntries(c.Universe.Entries(context.Background()))
			if len(errs) != 1 {
				t.Fatalf("got %d errors, want exactly 1; a bad document must not end like a complete one: %v", len(errs), errs)
			}
			if !strings.HasPrefix(errs[0].Error(), "supermarket: ") {
				t.Errorf("err = %q; want the package's supermarket: prefix", errs[0])
			}
		})
	}
}

func TestUniverseEntriesContextCancelMidStream(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/universe", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"apache2": {"1.0.0": {"location_path":"u1"}},`)
		w.(http.Flusher).Flush()
		// Hold the rest of the document back until the client goes away.
		<-r.Context().Done()
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var entries []UniverseEntry
	var errs []error
	for e, err := range c.Universe.Entries(ctx) {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		entries = append(entries, e)
		cancel()
	}
	if len(entries) != 1 {
		t.Errorf("got %d entries before cancel, want 1", len(entries))
	}
	if len(errs) != 1 {
		t.Fatalf("got %d errors, want 1; a cancelled stream must not look like a complete universe: %v", len(errs), errs)
	}
	if !errors.Is(errs[0], context.Canceled) {
		t.Errorf("err = %v; want errors.Is(err, context.Canceled) so callers can tell cancellation from a bad document", errs[0])
	}
}
