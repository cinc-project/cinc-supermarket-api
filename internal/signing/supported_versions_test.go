package signing

import "testing"

// supermarketSupportedSignVersions is the set of mixlib signing-protocol
// versions observed to work against the public Chef Supermarket. Signing with
// 1.3 (SHA-256) got a 401 on every share against supermarket.chef.io in
// production. The cause is unknown: upstream mixlib-authentication's verifier
// has a 1.3 branch and does not consult its deprecated SUPPORTED_VERSIONS
// list, so this is an observed limit, not a documented one.
//
// Don't add "1.3" here until a 1.3 share has been shown to succeed against
// the target Supermarket.
var supermarketSupportedSignVersions = map[string]bool{
	"1.0": true,
	"1.1": true,
}

// TestSignVersionIsAcceptedBySupermarket is the always-on, Ruby-free guard
// that would have failed the v1.3 (SHA-256) regression that 401'd every
// `cinc supermarket share`: it asserts we sign with a version observed to
// work in production. The byte-for-byte signer vector tests can't catch that
// class of bug, because a 1.3 signature is valid mixlib output; the failure
// only showed up against the live server.
func TestSignVersionIsAcceptedBySupermarket(t *testing.T) {
	if !supermarketSupportedSignVersions[SignVersion] {
		t.Fatalf("SignVersion = %q, which is not one of the versions observed to work against "+
			"supermarket.chef.io (%v). Signing with 1.3 got a 401 on every share in production "+
			"(cause unknown); prove a share succeeds with the new version before changing this guard.",
			SignVersion, keysOf(supermarketSupportedSignVersions))
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
