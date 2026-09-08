package supermarket

import (
	"regexp"
	"strconv"
	"strings"
)

// CompareVersions compares two cookbook version strings using a relaxed
// semver ordering and returns -1, 0, or 1 in the usual sense (a<b, a==b, a>b).
//
// Numeric base segments are compared numerically (so 1.10.0 > 1.2.0), missing
// trailing segments are treated as zero (1.0 == 1.0.0), and an empty string
// sorts below everything. A base segment that is not numeric is compared
// lexically and ranks above a numeric one, so a malformed version such as
// 1.0.x is ordered consistently rather than collapsing onto 1.0.0. A clean release outranks any pre-release of the same
// base; pre-release identifiers are compared per semver §11 (dot-separated,
// all-numeric identifiers compared numerically and ranking below alphanumeric
// ones, with a larger set of fields winning when all earlier ones are equal).
// Build metadata (everything after '+') is ignored. Supermarket versions are
// overwhelmingly plain "X.Y.Z", so this avoids pulling in a full semver
// dependency while still ordering the occasional pre-release sensibly.
func CompareVersions(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return -1
	}
	if b == "" {
		return 1
	}
	abase, apre := splitPrerelease(a)
	bbase, bpre := splitPrerelease(b)
	if c := compareBaseSegments(abase, bbase); c != 0 {
		return c
	}
	switch {
	case apre == "" && bpre == "":
		return 0
	case apre == "":
		return 1
	case bpre == "":
		return -1
	default:
		return comparePrerelease(apre, bpre)
	}
}

// LatestVersion returns the highest version in versions per CompareVersions,
// or "" when versions is empty.
func LatestVersion(versions []string) string {
	latest := ""
	for i, v := range versions {
		if i == 0 || CompareVersions(v, latest) > 0 {
			latest = v
		}
	}
	return latest
}

// versionish matches a bare version string in either the dotted or the
// underscore form Supermarket uses in its paths, with an optional
// pre-release or build suffix.
var versionish = regexp.MustCompile(`^[0-9]+(?:[._][0-9]+)*(?:[-+][0-9A-Za-z.\-]+)?$`)

// VersionFromURL extracts the version segment from a Supermarket version URL
// like https://supermarket.chef.io/api/v1/cookbooks/nginx/versions/12_0_4
// (optionally followed by /download or a query), returning "12.0.4".
// Supermarket encodes dots as underscores in these paths; either form is
// tolerated, so a bare "12.0.4" or "12_0_4" passes through unchanged.
//
// Input that is not a version returns "". Previously the underscore-to-dot
// rewrite was applied unconditionally to whatever was left after trimming,
// which turned "my_cookbook" into "my.cookbook" and a URL with no /versions/
// segment into "https:" — plausible-looking values that were never versions.
func VersionFromURL(s string) string {
	if s == "" {
		return ""
	}
	tail := s
	if i := strings.LastIndex(tail, "/versions/"); i >= 0 {
		tail = tail[i+len("/versions/"):]
	} else if strings.ContainsAny(tail, "/:") {
		// Shaped like a URL, but carries no version segment to extract.
		return ""
	}
	if i := strings.IndexAny(tail, "/?#"); i >= 0 {
		tail = tail[:i]
	}
	if !versionish.MatchString(tail) {
		return ""
	}
	return strings.ReplaceAll(tail, "_", ".")
}

// splitPrerelease separates a version into its numeric base and pre-release
// suffix. Build metadata (everything after a '+') is discarded, since semver
// §10 says it must be ignored for precedence.
func splitPrerelease(v string) (base, pre string) {
	v, _, _ = strings.Cut(v, "+")
	base, pre, _ = strings.Cut(v, "-")
	return base, pre
}

// compareBaseSegments compares the dot-separated numeric bases field by
// field. Fields are compared with the same rule as pre-release identifiers:
// numerically when both are numeric, and otherwise lexically with a numeric
// field ranking below an alphanumeric one (semver §11).
//
// The alternative — parsing each field with Atoi and treating a failure as
// zero — made every malformed field collapse onto 0, so "1.0.x" and "1.0.0"
// compared equal. A comparator that reports "equal" for strings that differ
// makes LatestVersion and any sort built on it unpredictable.
func compareBaseSegments(a, b string) int {
	ap := strings.Split(a, ".")
	bp := strings.Split(b, ".")
	n := max(len(ap), len(bp))
	for i := range n {
		if c := comparePrereleaseIdent(segment(ap, i), segment(bp, i)); c != 0 {
			return c
		}
	}
	return 0
}

// segment returns the i'th field, or "0" past the end so that a missing
// trailing field reads as zero and 1.0 equals 1.0.0.
func segment(parts []string, i int) string {
	if i >= len(parts) {
		return "0"
	}
	return parts[i]
}

// comparePrerelease compares two dot-separated pre-release strings per semver
// §11: identifiers are compared left to right; all-numeric identifiers compare
// numerically and rank below alphanumeric ones, and a larger set of fields wins
// when all preceding identifiers are equal.
func comparePrerelease(a, b string) int {
	ai := strings.Split(a, ".")
	bi := strings.Split(b, ".")
	for i := 0; i < len(ai) && i < len(bi); i++ {
		if c := comparePrereleaseIdent(ai[i], bi[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(ai) < len(bi):
		return -1
	case len(ai) > len(bi):
		return 1
	default:
		return 0
	}
}

func comparePrereleaseIdent(a, b string) int {
	an, aerr := strconv.Atoi(a)
	bn, berr := strconv.Atoi(b)
	switch {
	case aerr == nil && berr == nil:
		switch {
		case an < bn:
			return -1
		case an > bn:
			return 1
		default:
			return 0
		}
	case aerr == nil: // numeric identifiers rank below alphanumeric ones
		return -1
	case berr == nil:
		return 1
	default:
		return strings.Compare(a, b)
	}
}
