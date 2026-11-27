package supermarket_test

// This file holds the shape/type assertions shared by the two halves of the
// contract layer:
//
//   - contract_replay_test.go (always runs) decodes recorded golden bodies in
//     testdata/contract/ through our public types and runs these assertions —
//     a deterministic, network-free CI signal that the decoders still accept
//     real Supermarket payloads.
//   - contract_live_test.go (//go:build contract) hits the live
//     supermarket.chef.io read endpoints and runs the SAME assertions — the
//     drift alarm that fires when production diverges from the recordings.
//
// The assertions deliberately check shape, types, and required-field presence
// rather than volatile values (exact totals, download counts, version
// numbers) so the live suite doesn't flake as the catalog changes.

import (
	"strings"
	"testing"

	supermarket "github.com/cinc-project/cinc-supermarket-api"
)

func assertCookbookPage(t *testing.T, p supermarket.Page[supermarket.CookbookSummary], label string) {
	t.Helper()
	if p.Total <= 0 {
		t.Errorf("%s: Total = %d, want > 0", label, p.Total)
	}
	if len(p.Items) == 0 {
		t.Fatalf("%s: no items decoded", label)
	}
	for i, it := range p.Items {
		if it.Name == "" {
			t.Errorf("%s: items[%d].Name (cookbook_name) is empty", label, i)
		}
		if !strings.Contains(it.URL, "/cookbooks/") {
			t.Errorf("%s: items[%d].URL (cookbook) = %q, want a /cookbooks/ URL", label, i, it.URL)
		}
	}
}

func assertToolPage(t *testing.T, p supermarket.Page[supermarket.ToolSummary], label string) {
	t.Helper()
	if p.Total <= 0 {
		t.Errorf("%s: Total = %d, want > 0", label, p.Total)
	}
	if len(p.Items) == 0 {
		t.Fatalf("%s: no items decoded", label)
	}
	for i, it := range p.Items {
		if it.Name == "" {
			t.Errorf("%s: items[%d].Name (tool_name) is empty", label, i)
		}
		if !strings.Contains(it.URL, "/tools/") {
			t.Errorf("%s: items[%d].URL (tool) = %q, want a /tools/ URL", label, i, it.URL)
		}
	}
}

func assertCookbook(t *testing.T, cb *supermarket.Cookbook, wantName string) {
	t.Helper()
	if cb.Name != wantName {
		t.Errorf("Cookbook.Name = %q, want %q", cb.Name, wantName)
	}
	if cb.Maintainer == "" {
		t.Errorf("Cookbook.Maintainer is empty")
	}
	if !strings.Contains(cb.LatestVersion, "/versions/") {
		t.Errorf("Cookbook.LatestVersion = %q, want a /versions/ URL", cb.LatestVersion)
	}
	if len(cb.Versions) == 0 {
		t.Errorf("Cookbook.Versions is empty")
	}
	for i, v := range cb.Versions {
		if !strings.Contains(v, "/versions/") {
			t.Errorf("Cookbook.Versions[%d] = %q, want a /versions/ URL", i, v)
		}
	}
	if cb.CreatedAt.IsZero() {
		t.Errorf("Cookbook.CreatedAt did not decode (zero time)")
	}
	// Metrics.Downloads is the nested object our client exposes; a
	// long-lived cookbook has a positive all-time total.
	if cb.Metrics.Downloads.Total <= 0 {
		t.Errorf("Cookbook.Metrics.Downloads.Total = %d, want > 0", cb.Metrics.Downloads.Total)
	}
}

func assertCookbookVersion(t *testing.T, v *supermarket.CookbookVersion, wantVersion string) {
	t.Helper()
	if v.Version != wantVersion {
		t.Errorf("CookbookVersion.Version = %q, want %q", v.Version, wantVersion)
	}
	if v.License == "" {
		t.Errorf("CookbookVersion.License is empty")
	}
	if v.TarballSize <= 0 {
		t.Errorf("CookbookVersion.TarballSize = %d, want > 0", v.TarballSize)
	}
	if !strings.Contains(v.File, "/download") {
		t.Errorf("CookbookVersion.File = %q, want a /download URL", v.File)
	}
	if v.PublishedAt.IsZero() {
		t.Errorf("CookbookVersion.PublishedAt did not decode (zero time)")
	}
	if len(v.QualityMetrics) == 0 {
		t.Errorf("CookbookVersion.QualityMetrics is empty")
	}
	for i, qm := range v.QualityMetrics {
		if qm.Name == "" {
			t.Errorf("CookbookVersion.QualityMetrics[%d].Name is empty", i)
		}
	}
}

func assertUser(t *testing.T, u *supermarket.User, wantUsername string) {
	t.Helper()
	if u.Username != wantUsername {
		t.Errorf("User.Username = %q, want %q", u.Username, wantUsername)
	}
	if u.Name == "" {
		t.Errorf("User.Name is empty")
	}
	// The relationship groups are name->URL maps (the shape the contract
	// suite caught the client decoding wrongly). A long-lived account owns
	// at least one cookbook, and the values must be cookbook URLs.
	if len(u.Cookbooks.Owns) == 0 {
		t.Fatalf("User.Cookbooks.Owns is empty for %q", wantUsername)
	}
	for name, url := range u.Cookbooks.Owns {
		if name == "" {
			t.Errorf("User.Cookbooks.Owns has an empty cookbook name")
		}
		if !strings.Contains(url, "/cookbooks/") {
			t.Errorf("User.Cookbooks.Owns[%q] = %q, want a /cookbooks/ URL", name, url)
		}
	}
}

func assertUniverse(t *testing.T, u supermarket.Universe) {
	t.Helper()
	if len(u) == 0 {
		t.Fatalf("Universe decoded empty")
	}
	// Spot-check one cookbook/version entry: every entry must carry a
	// location type and path our resolver depends on.
	for name, versions := range u {
		if name == "" {
			t.Errorf("Universe has an empty cookbook name")
		}
		for ver, uv := range versions {
			if ver == "" {
				t.Errorf("Universe[%q] has an empty version key", name)
			}
			if uv.LocationType == "" {
				t.Errorf("Universe[%q][%q].LocationType is empty", name, ver)
			}
			if uv.LocationPath == "" {
				t.Errorf("Universe[%q][%q].LocationPath is empty", name, ver)
			}
			if !strings.HasSuffix(uv.DownloadURL, "/download") {
				t.Errorf("Universe[%q][%q].DownloadURL = %q, want the tarball's /download URL", name, ver, uv.DownloadURL)
			}
			return // one entry is enough; the map is large.
		}
	}
}

func assertContingents(t *testing.T, got []supermarket.Contingent, label string) {
	t.Helper()
	if len(got) == 0 {
		t.Fatalf("%s: no contingents decoded", label)
	}
	for i, c := range got {
		if c.Name == "" || c.Version == "" {
			t.Errorf("%s: [%d] = %+v, want name and version", label, i, c)
		}
		if !strings.Contains(c.URL, "/versions/") {
			t.Errorf("%s: [%d].URL = %q, want a cookbook version URL", label, i, c.URL)
		}
	}
}

func assertDeprecatedWithReplacement(t *testing.T, cb *supermarket.Cookbook) {
	t.Helper()
	if !cb.Deprecated {
		t.Fatalf("%s: Deprecated = false, want true", cb.Name)
	}
	if !strings.Contains(cb.Replacement, "/cookbooks/") {
		t.Errorf("%s: Replacement = %q, want the successor cookbook's URL", cb.Name, cb.Replacement)
	}
}

func assertHealth(t *testing.T, h *supermarket.Health) {
	t.Helper()
	if h.Status == "" {
		t.Error("Health.Status is empty")
	}
	for name, status := range map[string]string{
		"postgresql": h.PostgreSQL.Status, "sidekiq": h.Sidekiq.Status, "redis": h.Redis.Status,
	} {
		if status == "" {
			t.Errorf("Health.%s.Status is empty; the subsystem block did not decode", name)
		}
	}
	if len(h.Features) == 0 {
		t.Error("Health.Features is empty; the feature flags did not decode")
	}
}
