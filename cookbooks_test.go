package supermarket

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cinc-project/cinc-supermarket-api/internal/signing"
)

func TestCookbooksListReturnsPaginatedResults(t *testing.T) {
	var gotQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"total":42,"start":10,"items":[
			{"cookbook_name":"apache2","cookbook_description":"Web","cookbook":"u1","cookbook_maintainer":"alice"},
			{"cookbook_name":"nginx","cookbook_description":"Web","cookbook":"u2","cookbook_maintainer":"bob"}
		]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	page, _, err := c.Cookbooks.List(context.Background(), ListOptions{
		Start: 10, Items: 2, Order: "most_downloaded", User: "alice",
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.Total != 42 || page.Start != 10 || len(page.Items) != 2 {
		t.Errorf("page = %+v", page)
	}
	if page.Items[0].Name != "apache2" {
		t.Errorf("first item Name = %q", page.Items[0].Name)
	}
	if !strings.Contains(gotQuery, "start=10") ||
		!strings.Contains(gotQuery, "items=2") ||
		!strings.Contains(gotQuery, "order=most_downloaded") ||
		!strings.Contains(gotQuery, "user=alice") {
		t.Errorf("query = %q", gotQuery)
	}
	if !page.HasMore() {
		t.Error("HasMore should be true with start=10 + 2 items < total=42")
	}
}

func TestCookbooksGetReturnsFullRecord(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks/apache2", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"apache2","maintainer":"alice","latest_version":"1.2.0","metrics":{"downloads":{"total":99,"versions":{"1.2.0":99}},"followers":3}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	cb, _, err := c.Cookbooks.Get(context.Background(), "apache2")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cb.Name != "apache2" || cb.LatestVersion != "1.2.0" || cb.Metrics.Downloads.Total != 99 {
		t.Errorf("cb = %+v", cb)
	}
}

func TestCookbooksGetExposesURLsAndAdoption(t *testing.T) {
	// source_url, issues_url, and up_for_adoption are returned by the
	// real /cookbooks/:name endpoint; make sure they decode instead of
	// being silently dropped.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks/apache2", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"name":"apache2","maintainer":"alice","latest_version":"1.2.0",
			"source_url":"https://github.com/sous-chefs/apache2",
			"issues_url":"https://github.com/sous-chefs/apache2/issues",
			"up_for_adoption":true,
			"metrics":{"downloads":{"total":1}}
		}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	cb, _, err := c.Cookbooks.Get(context.Background(), "apache2")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cb.SourceURL != "https://github.com/sous-chefs/apache2" {
		t.Errorf("SourceURL = %q", cb.SourceURL)
	}
	if cb.IssuesURL != "https://github.com/sous-chefs/apache2/issues" {
		t.Errorf("IssuesURL = %q", cb.IssuesURL)
	}
	if !cb.UpForAdoption {
		t.Error("UpForAdoption = false, want true")
	}
}

func TestCookbooksGetVersionExposesSupportsAndQualityMetrics(t *testing.T) {
	// The version endpoint returns platform constraints under "supports"
	// (not "platforms"), plus "published_at" and a "quality_metrics"
	// array. All three were previously dropped or mis-mapped.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks/apache2/versions/1_2_0", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"version":"1.2.0","license":"Apache-2.0","cookbook":"u","file":"f",
			"published_at":"2026-03-22T06:55:12Z",
			"supports":{"ubuntu":">= 22.04","debian":">= 12.0"},
			"dependencies":{"apt":">= 0.0.0"},
			"quality_metrics":[
				{"name":"Collaborator Number","failed":false,"feedback":"passed with 2 collaborators."},
				{"name":"Version Tag","failed":true,"feedback":"Failure: needs a tag."}
			]
		}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	v, _, err := c.Cookbooks.GetVersion(context.Background(), "apache2", "1.2.0")
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if v.Supports["ubuntu"] != ">= 22.04" || v.Supports["debian"] != ">= 12.0" {
		t.Errorf("Supports = %+v", v.Supports)
	}
	if got := v.PublishedAt.UTC().Format(time.RFC3339); got != "2026-03-22T06:55:12Z" {
		t.Errorf("PublishedAt = %q, want 2026-03-22T06:55:12Z", got)
	}
	if len(v.QualityMetrics) != 2 {
		t.Fatalf("QualityMetrics len = %d, want 2", len(v.QualityMetrics))
	}
	if v.QualityMetrics[0].Name != "Collaborator Number" || v.QualityMetrics[0].Failed {
		t.Errorf("QualityMetrics[0] = %+v", v.QualityMetrics[0])
	}
	if !v.QualityMetrics[1].Failed || v.QualityMetrics[1].Feedback == "" {
		t.Errorf("QualityMetrics[1] = %+v", v.QualityMetrics[1])
	}
}

func TestCookbooksGetMapsNotFoundFromErrorCode(t *testing.T) {
	// Supermarket returns 400 with error_code=NOT_FOUND for missing
	// resources; the library should normalize that to ErrNotFound so
	// callers can use errors.Is without thinking about HTTP codes.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks/missing", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error_messages":["No such cookbook"],"error_code":"NOT_FOUND"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	_, _, err := c.Cookbooks.Get(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want errors.Is(err, ErrNotFound)", err)
	}
}

func TestCookbooksGetVersionConvertsDottedToUnderscore(t *testing.T) {
	var gotPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks/apache2/versions/1_2_3", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"version":"1.2.3","license":"Apache-2.0","cookbook":"u","file":"f"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	v, _, err := c.Cookbooks.GetVersion(context.Background(), "apache2", "1.2.3")
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if gotPath != "/api/v1/cookbooks/apache2/versions/1_2_3" {
		t.Errorf("path = %q, want underscore form", gotPath)
	}
	if v.Version != "1.2.3" {
		t.Errorf("Version = %q", v.Version)
	}
}

func TestCookbooksGetVersionAcceptsLatest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks/apache2/versions/latest", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"version":"2.0.0","cookbook":"u","file":"f"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	v, _, err := c.Cookbooks.GetVersion(context.Background(), "apache2", "latest")
	if err != nil {
		t.Fatalf("GetVersion latest: %v", err)
	}
	if v.Version != "2.0.0" {
		t.Errorf("Version = %q, want 2.0.0", v.Version)
	}
}

func TestCookbooksDownloadStreamsBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks/apache2/versions/1_0_0/download", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-gzip")
		_, _ = w.Write([]byte("tarball-bytes"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	body, _, err := c.Cookbooks.Download(context.Background(), "apache2", "1.0.0")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	defer body.Close()
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "tarball-bytes" {
		t.Errorf("body = %q", got)
	}
}

func TestCookbooksShareRequiresCredentials(t *testing.T) {
	srv := httptest.NewServer(http.NewServeMux())
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	_, _, err := c.Cookbooks.Share(context.Background(), "apache2", "Web Servers", strings.NewReader("tar"))
	if !errors.Is(err, ErrUnauthenticatedWrite) {
		t.Errorf("err = %v, want ErrUnauthenticatedWrite", err)
	}
}

func TestCookbooksShareSendsMultipartAndIsSigned(t *testing.T) {
	var (
		gotMethod      string
		gotContentType string
		gotMetaJSON    string
		gotTarball     []byte
		gotSign        string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotSign = r.Header.Get("X-Ops-Sign")
		mediaType, params, err := mime.ParseMediaType(gotContentType)
		if err != nil || mediaType != "multipart/form-data" {
			t.Fatalf("Content-Type = %q (%v)", gotContentType, err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(part)
			if err != nil {
				t.Fatal(err)
			}
			switch part.FormName() {
			case "cookbook":
				gotMetaJSON = string(body)
			case "tarball":
				gotTarball = body
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(fixture(t, "derived/share_created.json"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, true)

	res, _, err := c.Cookbooks.Share(context.Background(), "apache2", "Web Servers", bytes.NewReader([]byte("FAKE-TAR")))
	if err != nil {
		t.Fatalf("Share: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotSign != "algorithm=sha1;version=1.1;" {
		t.Errorf("X-Ops-Sign = %q, want signed request", gotSign)
	}
	var meta map[string]string
	if err := json.Unmarshal([]byte(gotMetaJSON), &meta); err != nil {
		t.Fatalf("cookbook part not JSON: %v\nbody: %s", err, gotMetaJSON)
	}
	if meta["category"] != "Web Servers" {
		t.Errorf("cookbook part = %v", meta)
	}
	if string(gotTarball) != "FAKE-TAR" {
		t.Errorf("tarball = %q", gotTarball)
	}
	if !strings.HasSuffix(res.URI, "/cookbooks/apache2") {
		t.Errorf("URI = %q, want the shared cookbook's URL", res.URI)
	}
}

// TestCookbooksShareContentHashCoversTarballOnly is a regression test
// for the multipart content-hash bug. Chef's signed-header protocol
// hashes only the uploaded file's bytes for X-Ops-Content-Hash, not the
// whole multipart envelope. Signing over the full envelope makes the
// server's RSA verification fail with a misleading 401
// AUTHENTICATION_FAILED ("invalid public/private key pair").
func TestCookbooksShareContentHashCoversTarballOnly(t *testing.T) {
	tarball := []byte("FAKE-TAR-BYTES")

	var (
		gotContentHash string
		gotFullBody    []byte
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks", func(w http.ResponseWriter, r *http.Request) {
		gotContentHash = r.Header.Get("X-Ops-Content-Hash")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		gotFullBody = body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(fixture(t, "derived/share_created.json"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, true)

	if _, _, err := c.Cookbooks.Share(context.Background(), "apache2", "Web Servers", bytes.NewReader(tarball)); err != nil {
		t.Fatalf("Share: %v", err)
	}

	b64 := func(b []byte) string {
		sum := sha1.Sum(b)
		return base64.StdEncoding.EncodeToString(sum[:])
	}
	wantTarballHash := b64(tarball)
	fullBodyHash := b64(gotFullBody)

	if gotContentHash != wantTarballHash {
		t.Errorf("X-Ops-Content-Hash = %q, want sha1(tarball) = %q", gotContentHash, wantTarballHash)
	}
	if gotContentHash == fullBodyHash {
		t.Errorf("X-Ops-Content-Hash = %q hashes the whole multipart body; it must hash the tarball only", gotContentHash)
	}
	// Sanity: the full multipart body really is larger than the tarball,
	// so the two hashes genuinely differ.
	if len(gotFullBody) <= len(tarball) {
		t.Fatalf("multipart body (%d bytes) not larger than tarball (%d bytes)", len(gotFullBody), len(tarball))
	}
}

// TestShareCanonicalRequestUsesTarballHash asserts the signed canonical
// block (the actual string that gets RSA-signed) carries the tarball-only
// content hash, matching what the server reconstructs and verifies.
func TestShareCanonicalRequestUsesTarballHash(t *testing.T) {
	tarball := []byte("FAKE-TAR-BYTES")

	body, gotTarball, _, err := buildShareBody("apache2", "Web Servers", bytes.NewReader(tarball))
	if err != nil {
		t.Fatalf("buildShareBody: %v", err)
	}
	if !bytes.Equal(gotTarball, tarball) {
		t.Fatalf("buildShareBody tarball = %q, want %q", gotTarball, tarball)
	}

	canon := signing.CanonicalRequest(signing.Request{
		Method: http.MethodPost, Path: "/api/v1/cookbooks", Body: gotTarball,
		UserID: "tester", Timestamp: "2024-01-01T00:00:00Z",
	})
	want := "X-Ops-Content-Hash:" + signing.ContentHash(tarball)
	if !strings.Contains(canon, want) {
		t.Errorf("canonical request missing tarball content hash\nwant line: %q\ngot:\n%s", want, canon)
	}
	if strings.Contains(canon, "X-Ops-Content-Hash:"+signing.ContentHash(body)) {
		t.Errorf("canonical request uses full multipart-body hash; must use tarball-only hash")
	}
}

func TestCookbooksDeleteSignsRequest(t *testing.T) {
	var sign string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks/apache2", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q, want DELETE", r.Method)
		}
		sign = r.Header.Get("X-Ops-Sign")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture(t, "derived/share_created.json"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, true)

	if _, _, err := c.Cookbooks.Delete(context.Background(), "apache2"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if sign != "algorithm=sha1;version=1.1;" {
		t.Errorf("X-Ops-Sign = %q, want signed request", sign)
	}
}

func TestCookbooksDeleteVersionRefusesWithoutCredentials(t *testing.T) {
	srv := httptest.NewServer(http.NewServeMux())
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	_, _, err := c.Cookbooks.DeleteVersion(context.Background(), "apache2", "1.0.0")
	if !errors.Is(err, ErrUnauthenticatedWrite) {
		t.Errorf("err = %v, want ErrUnauthenticatedWrite", err)
	}
}

func TestCookbooksListSendsNoQueryWhenOptionsEmpty(t *testing.T) {
	var rawQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks", func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"total":0,"start":0,"items":[]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	if _, _, err := c.Cookbooks.List(context.Background(), ListOptions{}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if rawQuery != "" {
		t.Errorf("RawQuery = %q, want empty when all options are zero", rawQuery)
	}
}

func TestCookbooksGetURLEscapesCookbookName(t *testing.T) {
	// Cookbook names should never contain spaces in practice, but the
	// library shouldn't break if a caller passes one — it should
	// URL-escape it rather than send a malformed request.
	var gotPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks/", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"name":"weird name"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	if _, _, err := c.Cookbooks.Get(context.Background(), "weird name"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	// The path on the wire is URL-decoded by net/http's mux; what we
	// assert is that the request reached the server intact rather than
	// dropping the suffix.
	if !strings.HasSuffix(gotPath, "weird name") {
		t.Errorf("server saw path %q, want it to include the cookbook name", gotPath)
	}
}

func TestCookbooksGetVersionAcceptsAlreadyUnderscoredForm(t *testing.T) {
	var gotPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks/apache2/versions/1_2_3", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"version":"1.2.3","cookbook":"u","file":"f"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	if _, _, err := c.Cookbooks.GetVersion(context.Background(), "apache2", "1_2_3"); err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/1_2_3") {
		t.Errorf("path = %q, want underscore form preserved (no double rewrite)", gotPath)
	}
}

func TestCookbooksDownloadReturnsErrorOnMissingVersion(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks/apache2/versions/9_9_9/download", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error_messages":["No such version"],"error_code":"NOT_FOUND"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	body, _, err := c.Cookbooks.Download(context.Background(), "apache2", "9.9.9")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if body != nil {
		t.Error("expected nil body on error path")
	}
}

func TestCookbooksDeleteMapsForbidden(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks/apache2", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error_messages":["not your cookbook"],"error_code":"FORBIDDEN"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, true)

	_, _, err := c.Cookbooks.Delete(context.Background(), "apache2")
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("err = %v, want ErrForbidden", err)
	}
}

func TestCookbooksShareWithEmptyTarballStillBuildsBody(t *testing.T) {
	// Some callers may want to "share" with no tarball (eg. test
	// scaffolding). The library should still produce a syntactically
	// valid multipart body and let the server reject it.
	var gotTarball []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/cookbooks", func(w http.ResponseWriter, r *http.Request) {
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatal(err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if part.FormName() == "tarball" {
				gotTarball, _ = io.ReadAll(part)
			}
		}
		_, _ = w.Write(fixture(t, "derived/share_created.json"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, true)

	if _, _, err := c.Cookbooks.Share(context.Background(), "apache2", "Other", bytes.NewReader(nil)); err != nil {
		t.Fatalf("Share with empty tarball: %v", err)
	}
	if len(gotTarball) != 0 {
		t.Errorf("server saw tarball bytes = %d, want 0", len(gotTarball))
	}
}

func TestCookbooksWriteOpsAllRefuseWithoutCredentials(t *testing.T) {
	srv := httptest.NewServer(http.NewServeMux())
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv, false)

	if _, _, err := c.Cookbooks.Share(context.Background(), "x", "Other", strings.NewReader("")); !errors.Is(err, ErrUnauthenticatedWrite) {
		t.Errorf("Share err = %v, want ErrUnauthenticatedWrite", err)
	}
	if _, _, err := c.Cookbooks.Delete(context.Background(), "x"); !errors.Is(err, ErrUnauthenticatedWrite) {
		t.Errorf("Delete err = %v, want ErrUnauthenticatedWrite", err)
	}
	if _, _, err := c.Cookbooks.DeleteVersion(context.Background(), "x", "1.0.0"); !errors.Is(err, ErrUnauthenticatedWrite) {
		t.Errorf("DeleteVersion err = %v, want ErrUnauthenticatedWrite", err)
	}
}
