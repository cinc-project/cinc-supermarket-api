package supermarket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"
)

// Cookbook is the full cookbook representation returned by GET /cookbooks/:name.
type Cookbook struct {
	Name          string          `json:"name"`
	Maintainer    string          `json:"maintainer"`
	Description   string          `json:"description"`
	Category      string          `json:"category"`
	LatestVersion string          `json:"latest_version"`
	ExternalURL   string          `json:"external_url"`
	SourceURL     string          `json:"source_url"`
	IssuesURL     string          `json:"issues_url"`
	AverageRating *float64        `json:"average_rating"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Deprecated    bool            `json:"deprecated"`
	UpForAdoption bool            `json:"up_for_adoption"`
	Versions      []string        `json:"versions"`
	Metrics       CookbookMetrics `json:"metrics"`
}

// CookbookMetrics is the metrics sub-object of a Cookbook.
type CookbookMetrics struct {
	Downloads     Downloads `json:"downloads"`
	Followers     int       `json:"followers"`
	Collaborators int       `json:"collaborators"`
}

// Downloads splits a cookbook's download counter into the all-time
// total and a per-version breakdown.
type Downloads struct {
	Total    int            `json:"total"`
	Versions map[string]int `json:"versions"`
}

// CookbookSummary is the lighter shape returned by list and search.
type CookbookSummary struct {
	Name        string `json:"cookbook_name"`
	Description string `json:"cookbook_description"`
	URL         string `json:"cookbook"`
	Maintainer  string `json:"cookbook_maintainer"`
}

// CookbookVersion is the full version representation returned by
// GET /cookbooks/:name/versions/:version.
type CookbookVersion struct {
	Version       string            `json:"version"`
	License       string            `json:"license"`
	TarballSize   int64             `json:"tarball_file_size"`
	AverageRating *float64          `json:"average_rating"`
	PublishedAt   time.Time         `json:"published_at"`
	Cookbook      string            `json:"cookbook"` // URL of the cookbook
	File          string            `json:"file"`     // URL of the tarball
	Dependencies  map[string]string `json:"dependencies"`
	// Supports maps platform name to a version constraint. The API
	// returns this under the "supports" key (mirroring metadata.rb).
	Supports       map[string]string `json:"supports"`
	QualityMetrics []QualityMetric   `json:"quality_metrics"`
}

// QualityMetric is one entry in a cookbook version's quality_metrics
// array: the Fieri/cookstyle evaluation results shown on the version
// page (collaborator count, version tag, cookstyle, no-binaries, etc.).
type QualityMetric struct {
	Name     string `json:"name"`
	Failed   bool   `json:"failed"`
	Feedback string `json:"feedback"`
}

// ListOptions configures GET /cookbooks.
type ListOptions struct {
	Start int
	Items int
	// Order, when set, is one of: recently_updated, recently_added,
	// most_downloaded, most_followed.
	Order string
	// User, when set, restricts the result set to cookbooks owned by
	// that Supermarket username.
	User string
}

// CookbooksService accesses the /cookbooks endpoints.
type CookbooksService struct{ client *Client }

// List paginates the cookbook index.
func (s *CookbooksService) List(ctx context.Context, opts ListOptions) (Page[CookbookSummary], *Response, error) {
	q := url.Values{}
	applyPageQuery(q, opts.Start, opts.Items)
	if opts.Order != "" {
		q.Set("order", opts.Order)
	}
	if opts.User != "" {
		q.Set("user", opts.User)
	}
	return doJSON[Page[CookbookSummary]](ctx, s.client, request{
		method: http.MethodGet,
		path:   withQuery("/api/v1/cookbooks", q),
	})
}

// Get returns the full record for a single cookbook by name.
func (s *CookbooksService) Get(ctx context.Context, name string) (*Cookbook, *Response, error) {
	if err := requireArg("cookbook name", name); err != nil {
		return nil, nil, err
	}
	cb, resp, err := doJSON[Cookbook](ctx, s.client, request{
		method: http.MethodGet,
		path:   "/api/v1/cookbooks/" + url.PathEscape(name),
	})
	if err != nil {
		return nil, resp, err
	}
	return &cb, resp, nil
}

// Contingent returns cookbooks that depend on the named cookbook.
func (s *CookbooksService) Contingent(ctx context.Context, name string) ([]CookbookSummary, *Response, error) {
	if err := requireArg("cookbook name", name); err != nil {
		return nil, nil, err
	}
	return doJSON[[]CookbookSummary](ctx, s.client, request{
		method: http.MethodGet,
		path:   "/api/v1/cookbooks/" + url.PathEscape(name) + "/contingent",
	})
}

// GetVersion returns a single cookbook version. The version may be
// "latest" or a Semver string in either dotted ("1.2.3") or underscore
// ("1_2_3") form; the dotted form is converted automatically.
func (s *CookbooksService) GetVersion(ctx context.Context, name, version string) (*CookbookVersion, *Response, error) {
	if err := requireArg("cookbook name", name); err != nil {
		return nil, nil, err
	}
	if err := requireArg("version", version); err != nil {
		return nil, nil, err
	}
	v, resp, err := doJSON[CookbookVersion](ctx, s.client, request{
		method: http.MethodGet,
		path:   "/api/v1/cookbooks/" + url.PathEscape(name) + "/versions/" + url.PathEscape(versionPath(version)),
	})
	if err != nil {
		return nil, resp, err
	}
	return &v, resp, nil
}

// Download fetches the gzipped tarball for a cookbook version. The
// caller owns closing the returned body. Like GetVersion the version
// may be "latest" or any Semver form.
//
// The download is not subject to the client's total-transaction timeout,
// which would otherwise truncate a large tarball mid-read. Bound it with
// ctx.
func (s *CookbooksService) Download(ctx context.Context, name, version string) (io.ReadCloser, *Response, error) {
	if err := requireArg("cookbook name", name); err != nil {
		return nil, nil, err
	}
	if err := requireArg("version", version); err != nil {
		return nil, nil, err
	}
	return s.client.stream(ctx,
		"/api/v1/cookbooks/"+url.PathEscape(name)+"/versions/"+url.PathEscape(versionPath(version))+"/download")
}

// Share uploads a new cookbook version. tarball is the gzipped tar of
// the cookbook directory (must contain metadata.json at its root);
// category is the Supermarket category string (e.g. "Web Servers").
// Requires Username and Key on the Client.
func (s *CookbooksService) Share(ctx context.Context, name, category string, tarball io.Reader) (*Cookbook, *Response, error) {
	if err := requireArg("cookbook name", name); err != nil {
		return nil, nil, err
	}
	if !s.client.canSign() {
		return nil, nil, ErrUnauthenticatedWrite
	}
	body, tarballBytes, contentType, err := buildShareBody(name, category, tarball)
	if err != nil {
		return nil, nil, err
	}
	cb, resp, err := doJSON[Cookbook](ctx, s.client, request{
		method:      http.MethodPost,
		path:        "/api/v1/cookbooks",
		body:        body,
		contentType: contentType,
		sign:        true,
		// Chef signs multipart file uploads over the tarball bytes
		// alone, not the whole multipart envelope. See request.signBody.
		signBody: tarballBytes,
	})
	if err != nil {
		return nil, resp, err
	}
	return &cb, resp, nil
}

// Delete removes an entire cookbook from Supermarket. The caller must
// own the cookbook (or be a Supermarket admin).
func (s *CookbooksService) Delete(ctx context.Context, name string) (*Cookbook, *Response, error) {
	if err := requireArg("cookbook name", name); err != nil {
		return nil, nil, err
	}
	cb, resp, err := doJSON[Cookbook](ctx, s.client, request{
		method: http.MethodDelete,
		path:   "/api/v1/cookbooks/" + url.PathEscape(name),
		sign:   true,
	})
	if err != nil {
		return nil, resp, err
	}
	return &cb, resp, nil
}

// DeleteVersion removes a single version of a cookbook.
func (s *CookbooksService) DeleteVersion(ctx context.Context, name, version string) (*CookbookVersion, *Response, error) {
	if err := requireArg("cookbook name", name); err != nil {
		return nil, nil, err
	}
	if err := requireArg("version", version); err != nil {
		return nil, nil, err
	}
	v, resp, err := doJSON[CookbookVersion](ctx, s.client, request{
		method: http.MethodDelete,
		path:   "/api/v1/cookbooks/" + url.PathEscape(name) + "/versions/" + url.PathEscape(versionPath(version)),
		sign:   true,
	})
	if err != nil {
		return nil, resp, err
	}
	return &v, resp, nil
}

// buildShareBody assembles the multipart/form-data body the share
// endpoint expects: a JSON "cookbook" part carrying the category, and
// a "tarball" file part with the gzipped tar. Returns the full body,
// the raw tarball bytes, and the boundary-bearing Content-Type. The
// body is buffered into memory so it can be sent as the HTTP payload;
// the tarball bytes are returned separately because Chef's signed-header
// protocol hashes the file part alone (not the whole multipart envelope)
// for X-Ops-Content-Hash. For very large tarballs callers will need to
// keep this in-memory buffering in mind.
func buildShareBody(name, category string, tarball io.Reader) (body []byte, tarballBytes []byte, contentType string, err error) {
	tarballBytes, err = io.ReadAll(tarball)
	if err != nil {
		return nil, nil, "", fmt.Errorf("supermarket: read tarball: %w", err)
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	meta, err := json.Marshal(map[string]string{"category": category})
	if err != nil {
		return nil, nil, "", fmt.Errorf("supermarket: marshal share metadata: %w", err)
	}
	if err := w.WriteField("cookbook", string(meta)); err != nil {
		return nil, nil, "", fmt.Errorf("supermarket: write share metadata: %w", err)
	}
	tw, err := w.CreateFormFile("tarball", name+".tgz")
	if err != nil {
		return nil, nil, "", fmt.Errorf("supermarket: create tarball part: %w", err)
	}
	if _, err := tw.Write(tarballBytes); err != nil {
		return nil, nil, "", fmt.Errorf("supermarket: copy tarball: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, nil, "", fmt.Errorf("supermarket: close multipart writer: %w", err)
	}
	return buf.Bytes(), tarballBytes, w.FormDataContentType(), nil
}
