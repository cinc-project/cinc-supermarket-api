package supermarket

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// blankArgServer stands in for Supermarket and records whether anything
// reached it. A rejected argument must never produce a request.
func blankArgServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, `{"total":0,"start":0,"items":[]}`)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// TestBlankPathArgumentsAreRejected — a blank name or version is not a
// harmless 404. It is interpolated straight into the URL and addresses the
// *collection* endpoint: GET /api/v1/cookbooks/ returns the cookbook list,
// which decodes into a zero-valued Cookbook with a nil error, so the caller
// silently receives an empty record instead of a failure. The signed DELETE
// variants are worse — DELETE /api/v1/cookbooks/ is a destructive call against
// an endpoint the caller never meant to address.
func TestBlankPathArgumentsAreRejected(t *testing.T) {
	srv, hits := blankArgServer(t)
	c := newTestClient(t, srv, true)
	ctx := context.Background()

	for _, blank := range []string{"", "   "} {
		calls := map[string]func() error{
			"Cookbooks.Get":           func() error { _, _, err := c.Cookbooks.Get(ctx, blank); return err },
			"Cookbooks.Contingent":    func() error { _, _, err := c.Cookbooks.Contingent(ctx, blank); return err },
			"Cookbooks.GetVersion":    func() error { _, _, err := c.Cookbooks.GetVersion(ctx, blank, "1.0.0"); return err },
			"Cookbooks.GetVersion/v":  func() error { _, _, err := c.Cookbooks.GetVersion(ctx, "nginx", blank); return err },
			"Cookbooks.Download":      func() error { _, _, err := c.Cookbooks.Download(ctx, blank, "1.0.0"); return err },
			"Cookbooks.Download/v":    func() error { _, _, err := c.Cookbooks.Download(ctx, "nginx", blank); return err },
			"Cookbooks.Delete":        func() error { _, _, err := c.Cookbooks.Delete(ctx, blank); return err },
			"Cookbooks.DeleteVersion": func() error { _, _, err := c.Cookbooks.DeleteVersion(ctx, blank, "1.0.0"); return err },
			"Cookbooks.DelVersion/v":  func() error { _, _, err := c.Cookbooks.DeleteVersion(ctx, "nginx", blank); return err },
			"Cookbooks.Share": func() error {
				_, _, err := c.Cookbooks.Share(ctx, blank, "Other", bytes.NewReader([]byte("t")))
				return err
			},
			"Tools.Get": func() error { _, _, err := c.Tools.Get(ctx, blank); return err },
			"Users.Get": func() error { _, _, err := c.Users.Get(ctx, blank); return err },
		}
		for name, call := range calls {
			t.Run(name+"/"+blank, func(t *testing.T) {
				err := call()
				if err == nil {
					t.Fatalf("%s(%q) returned nil error", name, blank)
				}
				if !errors.Is(err, ErrInvalidArgument) {
					t.Errorf("%s(%q) error = %v, want it to wrap ErrInvalidArgument", name, blank, err)
				}
			})
		}
	}

	if n := hits.Load(); n != 0 {
		t.Errorf("a rejected argument still reached the server %d time(s)", n)
	}
}

// TestBlankArgumentRejectionPrecedesSigning — the write endpoints must refuse
// a blank name before they sign anything, so the check also holds for a client
// that has no credentials at all.
func TestBlankArgumentRejectionPrecedesSigning(t *testing.T) {
	srv, hits := blankArgServer(t)
	c := newTestClient(t, srv, false) // unsigned

	if _, _, err := c.Cookbooks.Delete(context.Background(), ""); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("Delete(\"\") on an unsigned client = %v, want ErrInvalidArgument", err)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("blank-name Delete reached the server %d time(s)", n)
	}
}
