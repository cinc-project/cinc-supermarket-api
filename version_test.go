package supermarket

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		// Plain numeric ordering.
		{"2.0.0", "1.9.9", 1},
		{"1.0.0", "1.0.0", 0},
		{"1.2.0", "1.10.0", -1}, // numeric, not lexical
		{"1.0", "1.0.0", 0},     // implied-zero patch
		{"", "1.0.0", -1},       // empty sorts lowest
		{"", "", 0},

		// A clean release outranks any prerelease of the same base.
		{"1.0.0", "1.0.0-beta", 1},

		// Dot-separated numeric prerelease identifiers compare numerically.
		{"1.0.0-alpha.10", "1.0.0-alpha.2", 1},
		{"1.0.0-alpha.1.1", "1.0.0-alpha.1", 1},

		// An un-dotted identifier is one alphanumeric token, compared lexically
		// per semver §11 (so "beta2" outranks "beta10").
		{"1.0.0-beta10", "1.0.0-beta2", -1},

		// Numeric prerelease identifiers rank below alphanumeric ones.
		{"1.0.0-1", "1.0.0-alpha", -1},

		// Build metadata is ignored for precedence, including alongside a
		// pre-release (equal pre-release, differing build metadata -> equal).
		{"1.0.0+build5", "1.0.0", 0},
		{"1.0.0+build5", "1.0.0+build2", 0},
		{"1.0.0-beta+x", "1.0.0-beta+y", 0},

		// A non-numeric base segment must not collapse to zero: "1.0.x" and
		// "1.0.0" are different versions and have to compare that way, or
		// sorting and dedup silently conflate them. Following semver §11, a
		// numeric identifier ranks below an alphanumeric one.
		{"1.0.x", "1.0.0", 1},
		{"2.0.0", "1.0.x", 1},
		{"1.0.x", "1.0.y", -1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := CompareVersions(c.b, c.a); got != -c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d (antisymmetry)", c.b, c.a, got, -c.want)
		}
	}
}

// TestCompareVersionsDistinguishesMalformedSegments — the equality above is
// the property that matters: a comparator that reports "equal" for strings
// that differ makes LatestVersion and any sort built on it unpredictable.
func TestCompareVersionsDistinguishesMalformedSegments(t *testing.T) {
	if CompareVersions("1.0.x", "1.0.0") == 0 {
		t.Error(`CompareVersions("1.0.x", "1.0.0") = 0; distinct versions must not compare equal`)
	}
	if got := LatestVersion([]string{"1.0.0", "1.0.x"}); got == "" {
		t.Error("LatestVersion returned empty for a list containing a malformed version")
	}
}

func TestLatestVersion(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"picks numeric max", []string{"1.0.0", "2.3.1", "2.3.0", "0.9.9"}, "2.3.1"},
		{"release beats prerelease", []string{"1.0.0-rc1", "1.0.0", "1.0.0-beta"}, "1.0.0"},
		{"single", []string{"3.1.4"}, "3.1.4"},
		{"empty", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := LatestVersion(c.in); got != c.want {
				t.Fatalf("LatestVersion(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestVersionFromURL(t *testing.T) {
	cases := map[string]string{
		"https://supermarket.chef.io/api/v1/cookbooks/nginx/versions/12_0_4":          "12.0.4",
		"https://supermarket.chef.io/api/v1/cookbooks/nginx/versions/12_0_4/download": "12.0.4",
		"https://supermarket.chef.io/api/v1/cookbooks/nginx/versions/1_2_3?x=1":       "1.2.3",
		"12.0.4": "12.0.4", // bare version passes through
		"12_0_4": "12.0.4", // underscore form normalized
		"":       "",

		// Input that is not a version must not be mangled into one. The
		// blanket underscore->dot rewrite turned any identifier into a
		// plausible-looking version string.
		"my_cookbook": "",
		"https://supermarket.chef.io/api/v1/cookbooks/my_cb": "",
		"https://supermarket.chef.io/api/v1/cookbooks":       "",
		"not a version": "",
	}
	for in, want := range cases {
		if got := VersionFromURL(in); got != want {
			t.Errorf("VersionFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}
