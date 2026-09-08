package supermarket

import (
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"
	"time"
)

func TestNewClientDefaultsBaseURL(t *testing.T) {
	c, err := NewClient(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.baseURL.String(); got != DefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", got, DefaultBaseURL)
	}
	if c.canSign() {
		t.Error("canSign should be false for an anonymous client")
	}
}

func TestNewClientRejectsInvalidBaseURL(t *testing.T) {
	if _, err := NewClient(Config{BaseURL: "not a url"}); err == nil {
		t.Error("expected an error for a malformed BaseURL")
	}
}

func TestCanSignRequiresBothUsernameAndKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	noUser, _ := NewClient(Config{Key: key})
	if noUser.canSign() {
		t.Error("canSign should be false without a Username")
	}

	noKey, _ := NewClient(Config{Username: "me"})
	if noKey.canSign() {
		t.Error("canSign should be false without a Key")
	}

	full, _ := NewClient(Config{Username: "me", Key: key})
	if !full.canSign() {
		t.Error("canSign should be true with both Username and Key")
	}
}

func TestParseKeyErrorMessagePrefixed(t *testing.T) {
	_, err := ParseKey([]byte("not a pem block"))
	if err == nil || !strings.Contains(err.Error(), "supermarket:") {
		t.Errorf("err = %v, want a supermarket-prefixed error", err)
	}
}

func TestNewClientTrimsTrailingSlashFromBaseURL(t *testing.T) {
	c, err := NewClient(Config{BaseURL: "https://example.test/"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.baseURL.String(); got != "https://example.test" {
		t.Errorf("baseURL = %q, want trailing slash stripped", got)
	}
}

func TestNewClientRejectsURLWithoutHost(t *testing.T) {
	if _, err := NewClient(Config{BaseURL: "/path-only"}); err == nil {
		t.Error("expected an error for a BaseURL with no host")
	}
}

func TestNewClientAttachesAllServices(t *testing.T) {
	c, err := NewClient(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Cookbooks == nil || c.Search == nil || c.Tools == nil ||
		c.Users == nil || c.Universe == nil || c.Health == nil {
		t.Error("expected every service field to be wired up")
	}
}

func TestClientTimestampIsRFC3339UTC(t *testing.T) {
	c, _ := NewClient(Config{})
	c.clock = func() time.Time { return time.Unix(1700000000, 0).UTC() }
	got := c.timestamp()
	want := "2023-11-14T22:13:20Z"
	if got != want {
		t.Errorf("timestamp = %q, want %q", got, want)
	}
}

// TestNewClientRejectsUnusableBaseURL — only base.Host was checked, so a URL
// that net/url parses but net/http can never fetch was accepted at
// construction and failed later, per call, with a confusing error. A base
// carrying a query or fragment is worse than useless: paths are appended by
// string concatenation, so "http://x?a=1" + "/api/v1/cookbooks" produces
// "http://x?a=1/api/v1/cookbooks".
func TestNewClientRejectsUnusableBaseURL(t *testing.T) {
	for _, raw := range []string{
		"ftp://supermarket.test",
		"file:///etc/passwd",
		"://bad",
		"supermarket.test",    // no scheme at all
		"http://x?a=1",        // query would be concatenated into the path
		"https://x/base#frag", // ditto for a fragment
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := NewClient(Config{BaseURL: raw}); err == nil {
				t.Errorf("NewClient accepted an unusable BaseURL %q", raw)
			}
		})
	}
}

// TestNewClientAcceptsUsableBaseURL scopes the rejection above.
func TestNewClientAcceptsUsableBaseURL(t *testing.T) {
	for _, raw := range []string{
		"https://supermarket.chef.io",
		"https://supermarket.chef.io/",
		"http://localhost:8080",
		"https://internal.test/supermarket",
		"HTTPS://Supermarket.Test", // scheme comparison is case-insensitive
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := NewClient(Config{BaseURL: raw}); err != nil {
				t.Errorf("NewClient(%q) = %v, want it accepted", raw, err)
			}
		})
	}
}
