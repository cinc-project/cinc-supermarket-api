package supermarket

// Tests in this file serve real Supermarket response bodies (recorded from
// production, or derived from upstream's views for the write endpoints) and
// assert the client decodes them. Each one covers a place where the client
// previously disagreed with the server while its hand-written mocks agreed
// with the client.

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// TestContingentDecodesProductionShape — /contingent returns
// {"contingents":[{name,version,url}]}, not an array of cookbook summaries.
// Decoding it as []CookbookSummary failed on every call.
func TestContingentDecodesProductionShape(t *testing.T) {
	srv, _ := serveFixture(t, "/api/v1/cookbooks/yum/contingent", http.StatusOK, "cookbook_contingent_yum.json")
	c := newTestClient(t, srv, false)

	got, _, err := c.Cookbooks.Contingent(context.Background(), "yum")
	if err != nil {
		t.Fatalf("Contingent on the production body: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d contingents, want the 3 in the fixture", len(got))
	}
	first := got[0]
	if first.Name != "CVE-2015-0235" || first.Version != "1.0" {
		t.Errorf("first = %+v, want CVE-2015-0235 1.0", first)
	}
	if !strings.Contains(first.URL, "/cookbooks/cve-2015-0235/versions/1.0") {
		t.Errorf("URL = %q, want the dependent cookbook version's URL", first.URL)
	}
}

// TestShareReturnsUploadedCookbookURI — a successful share answers 201 with
// only {"uri": ...}. Decoding that into a Cookbook handed callers a record
// with every field empty and no error.
func TestShareReturnsUploadedCookbookURI(t *testing.T) {
	srv, _ := serveFixture(t, "/api/v1/cookbooks", http.StatusCreated, "derived/share_created.json")
	c := newTestClient(t, srv, true)

	res, _, err := c.Cookbooks.Share(context.Background(), "apache2", "Web Servers", bytes.NewReader([]byte("tar")))
	if err != nil {
		t.Fatalf("Share: %v", err)
	}
	if res.URI != "https://supermarket.chef.io/api/v1/cookbooks/apache2" {
		t.Errorf("URI = %q, want the uploaded cookbook's API URL from the 201 body", res.URI)
	}
}

// TestCookbookDecodesReplacement — a deprecated cookbook names its
// successor in "replacement"; dropping it hid the one actionable piece of a
// deprecation from callers.
func TestCookbookDecodesReplacement(t *testing.T) {
	srv, _ := serveFixture(t, "/api/v1/cookbooks/mongodb", http.StatusOK, "cookbook_mongodb_deprecated.json")
	c := newTestClient(t, srv, false)

	cb, _, err := c.Cookbooks.Get(context.Background(), "mongodb")
	if err != nil {
		t.Fatal(err)
	}
	if !cb.Deprecated || cb.Replacement != "https://supermarket.chef.io/api/v1/cookbooks/sc-mongodb" {
		t.Errorf("Deprecated=%v Replacement=%q, want true and the sc-mongodb URL", cb.Deprecated, cb.Replacement)
	}
}

// TestUniverseDecodesDownloadURL — each universe entry carries the tarball's
// download_url, the field a dependency resolver needs to fetch what it
// resolved. location_type is "opscode" (kept for Berkshelf), not "supermarket".
func TestUniverseDecodesDownloadURL(t *testing.T) {
	srv, _ := serveFixture(t, "/universe", http.StatusOK, "universe_excerpt.json")
	c := newTestClient(t, srv, false)

	u, _, err := c.Universe.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	v := u["dynomite"]["0.1.0"]
	if v.DownloadURL != "https://supermarket.chef.io:443/api/v1/cookbooks/dynomite/versions/0.1.0/download" {
		t.Errorf("DownloadURL = %q, want the version's /download URL", v.DownloadURL)
	}
	if v.LocationType != "opscode" {
		t.Errorf("LocationType = %q, want opscode as production sends", v.LocationType)
	}
}

// TestHealthDecodesSubsystems — /health reports each backing service, not
// just an overall status; a monitor needs to know which one is down.
func TestHealthDecodesSubsystems(t *testing.T) {
	srv, _ := serveFixture(t, "/api/v1/health", http.StatusOK, "health.json")
	c := newTestClient(t, srv, false)

	h, _, err := c.Health.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.Status != "ok" {
		t.Errorf("Status = %q", h.Status)
	}
	if h.Sidekiq.Status != "reachable" || h.PostgreSQL.Status != "reachable" || h.Redis.Status != "reachable" {
		t.Errorf("subsystem statuses = %q/%q/%q, want reachable", h.Sidekiq.Status, h.PostgreSQL.Status, h.Redis.Status)
	}
	if h.PostgreSQL.Connections <= 0 || h.Redis.UsedMemory <= 0 || h.Sidekiq.TotalProcessed <= 0 {
		t.Errorf("subsystem metrics not decoded: %+v", h)
	}
	if !h.Features["tools"] {
		t.Errorf("Features = %v, want tools enabled as recorded", h.Features)
	}
}

// TestToolDecodesUpForAdoption covers the tool show field the client dropped.
func TestToolDecodesUpForAdoption(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tools/knife-x", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"knife-x","slug":"knife-x","up_for_adoption":true}`))
	})
	srv := newServer(t, mux)
	c := newTestClient(t, srv, false)

	tool, _, err := c.Tools.Get(context.Background(), "knife-x")
	if err != nil {
		t.Fatal(err)
	}
	if !tool.UpForAdoption {
		t.Error("UpForAdoption = false, want true from the body")
	}
}

// TestUserDecodesToolCollaborations — users/show emits tools.collaborates
// alongside tools.owns.
func TestUserDecodesToolCollaborations(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/users/alice", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"username":"alice","tools":{"collaborates":{"knife-x":"https://s/api/v1/tools/knife-x"}}}`))
	})
	srv := newServer(t, mux)
	c := newTestClient(t, srv, false)

	u, _, err := c.Users.Get(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if u.Tools.Collaborates["knife-x"] == "" {
		t.Errorf("Tools.Collaborates = %v, want knife-x", u.Tools.Collaborates)
	}
}

// TestListSendsPlatforms — /cookbooks filters on platforms[] (Rails array
// syntax, one key per value).
func TestListSendsPlatforms(t *testing.T) {
	srv, last := serveFixture(t, "/api/v1/cookbooks", http.StatusOK, "cookbooks_list.json")
	c := newTestClient(t, srv, false)

	if _, _, err := c.Cookbooks.List(context.Background(), ListOptions{Platforms: []string{"debian", "ubuntu"}}); err != nil {
		t.Fatal(err)
	}
	got := last.URL.Query()["platforms[]"]
	if len(got) != 2 || got[0] != "debian" || got[1] != "ubuntu" {
		t.Errorf("platforms[] = %v, want [debian ubuntu]; the server reads only the array form", got)
	}
}

// TestToolsSearchSendsTypeAndOrder — /tools-search filters on type and
// orders on order; SearchOptions could carry neither.
func TestToolsSearchSendsTypeAndOrder(t *testing.T) {
	srv, last := serveFixture(t, "/api/v1/tools-search", http.StatusOK, "tools_search_kitchen.json")
	c := newTestClient(t, srv, false)

	p, _, err := c.Tools.Search(context.Background(), ToolSearchOptions{
		Q: "kitchen", Type: ToolTypeKitchenDriver, Order: ToolOrderRecentlyAdded,
	})
	if err != nil {
		t.Fatal(err)
	}
	q := last.URL.Query()
	if q.Get("q") != "kitchen" || q.Get("type") != "kitchen_driver" || q.Get("order") != "recently_added" {
		t.Errorf("query = %v, want q, type=kitchen_driver and order=recently_added", q)
	}
	if len(p.Items) == 0 {
		t.Error("no items decoded from the recorded tools-search body")
	}
}

// TestErrorCodesMapToSentinels covers every error_code upstream defines,
// plus 429. Each row is a body the server actually produces.
func TestErrorCodesMapToSentinels(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		fixture string
		want    error
	}{
		{"not found", 404, "error_not_found.json", ErrNotFound},
		{"conflict deleting the only version", 409, "derived/error_conflict_only_version.json", ErrConflict},
		{"bad signature (400)", 400, "derived/error_authentication_failed.json", ErrUnauthorized},
		{"bad key (401)", 401, "derived/error_bad_key.json", ErrUnauthorized},
		{"missing tarball part", 400, "derived/error_missing_tarball.json", ErrInvalidData},
		{"destroy not allowed", 403, "derived/error_forbidden_destroy.json", ErrForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := serveFixture(t, "/api/v1/cookbooks/x", tc.status, tc.fixture)
			c := newTestClient(t, srv, true)
			_, _, err := c.Cookbooks.Delete(context.Background(), "x")
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want errors.Is(err, %v)", err, tc.want)
			}
		})
	}

	t.Run("missing required data", func(t *testing.T) {
		er := newErrorResponse("POST", "/p", 400, []byte(`{"error_code":"MISSING_REQUIRED_DATA","error_messages":["x"]}`))
		if !errors.Is(er, ErrInvalidData) {
			t.Errorf("MISSING_REQUIRED_DATA does not map to ErrInvalidData")
		}
	})
	t.Run("rate limited", func(t *testing.T) {
		er := newErrorResponse("GET", "/p", http.StatusTooManyRequests, nil)
		if !errors.Is(er, ErrRateLimited) {
			t.Errorf("429 does not map to ErrRateLimited")
		}
	})
}
