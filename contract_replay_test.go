package supermarket_test

// Replay half of the contract layer: decode the recorded golden bodies under
// testdata/contract/ through our public types and run the shared assertions.
// This always runs in the default `go test ./...` — no network, no tags — so
// CI gets a deterministic signal that the decoders still accept real
// Supermarket payloads. The golden bodies were captured from
// supermarket.chef.io; refresh them by re-running the live contract suite's
// recordings (see README "Contract testing").

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	supermarket "github.com/cinc-project/cinc-supermarket-api"
)

func readGolden[T any](t *testing.T, name string) T {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "contract", name))
	if err != nil {
		t.Fatalf("reading golden %s: %v", name, err)
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decoding golden %s through public type %T: %v", name, out, err)
	}
	return out
}

func TestContractReplay_CookbooksList(t *testing.T) {
	p := readGolden[supermarket.Page[supermarket.CookbookSummary]](t, "cookbooks_list.json")
	assertCookbookPage(t, p, "cookbooks_list.json")
}

func TestContractReplay_Search(t *testing.T) {
	p := readGolden[supermarket.Page[supermarket.CookbookSummary]](t, "search_nginx.json")
	assertCookbookPage(t, p, "search_nginx.json")
}

func TestContractReplay_Cookbook(t *testing.T) {
	cb := readGolden[supermarket.Cookbook](t, "cookbook_nginx.json")
	assertCookbook(t, &cb, "nginx")
}

func TestContractReplay_CookbookVersion(t *testing.T) {
	v := readGolden[supermarket.CookbookVersion](t, "cookbook_version_nginx_12_3_1.json")
	assertCookbookVersion(t, &v, "12.3.1")
}

func TestContractReplay_ToolsList(t *testing.T) {
	p := readGolden[supermarket.Page[supermarket.ToolSummary]](t, "tools_list.json")
	assertToolPage(t, p, "tools_list.json")
}

func TestContractReplay_User(t *testing.T) {
	u := readGolden[supermarket.User](t, "user_sous-chefs.json")
	assertUser(t, &u, "sous-chefs")
}

func TestContractReplay_Contingent(t *testing.T) {
	env := readGolden[struct {
		Contingents []supermarket.Contingent `json:"contingents"`
	}](t, "cookbook_contingent_yum.json")
	assertContingents(t, env.Contingents, "cookbook_contingent_yum.json")
}

func TestContractReplay_DeprecatedCookbook(t *testing.T) {
	cb := readGolden[supermarket.Cookbook](t, "cookbook_mongodb_deprecated.json")
	assertDeprecatedWithReplacement(t, &cb)
}

func TestContractReplay_ToolsSearch(t *testing.T) {
	p := readGolden[supermarket.Page[supermarket.ToolSummary]](t, "tools_search_kitchen.json")
	assertToolPage(t, p, "tools_search_kitchen.json")
}

func TestContractReplay_Health(t *testing.T) {
	h := readGolden[supermarket.Health](t, "health.json")
	assertHealth(t, &h)
}

func TestContractReplay_Universe(t *testing.T) {
	assertUniverse(t, readGolden[supermarket.Universe](t, "universe_excerpt.json"))
}

// decodeStrict decodes the named golden body with unknown fields disallowed.
func decodeStrict[T any](t *testing.T, name string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "contract", name))
	if err != nil {
		t.Fatalf("reading golden %s: %v", name, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var out T
	if err := dec.Decode(&out); err != nil {
		t.Errorf("%s: %v\nSupermarket sends a field %T does not map, so callers never see it. "+
			"Add it to the type, or record here why it is deliberately dropped.", name, err, out)
	}
}

// TestContractReplay_EveryFieldIsMapped decodes every golden body with
// unknown fields disallowed. The ordinary decoders silently drop fields they
// don't know, which is how download_url, replacement, up_for_adoption and the
// health subsystems went unnoticed. When a re-recorded body gains a field,
// this fails and names it.
func TestContractReplay_EveryFieldIsMapped(t *testing.T) {
	type contingents struct {
		Contingents []supermarket.Contingent `json:"contingents"`
	}
	cases := map[string]func(*testing.T, string){
		"cookbooks_list.json":                decodeStrict[supermarket.Page[supermarket.CookbookSummary]],
		"search_nginx.json":                  decodeStrict[supermarket.Page[supermarket.CookbookSummary]],
		"cookbook_nginx.json":                decodeStrict[supermarket.Cookbook],
		"cookbook_mongodb_deprecated.json":   decodeStrict[supermarket.Cookbook],
		"cookbook_version_nginx_12_3_1.json": decodeStrict[supermarket.CookbookVersion],
		"cookbook_contingent_yum.json":       decodeStrict[contingents],
		"tools_list.json":                    decodeStrict[supermarket.Page[supermarket.ToolSummary]],
		"tools_search_kitchen.json":          decodeStrict[supermarket.Page[supermarket.ToolSummary]],
		"tool_kitchen-noderings.json":        decodeStrict[supermarket.Tool],
		"user_sous-chefs.json":               decodeStrict[supermarket.User],
		"health.json":                        decodeStrict[supermarket.Health],
		"universe_excerpt.json":              decodeStrict[supermarket.Universe],
		"derived/share_created.json":         decodeStrict[supermarket.ShareResult],
	}
	for name, decode := range cases {
		t.Run(name, func(t *testing.T) { decode(t, name) })
	}

	// Every success-body golden must be in the table, so a newly recorded
	// fixture can't skip the check. Error bodies are decoded by
	// newErrorResponse and covered by the error-mapping tests.
	for _, dir := range []string{"", "derived"} {
		entries, err := os.ReadDir(filepath.Join("testdata", "contract", dir))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := filepath.ToSlash(filepath.Join(dir, e.Name()))
			if e.IsDir() || filepath.Ext(name) != ".json" || strings.HasPrefix(e.Name(), "error_") {
				continue
			}
			if _, ok := cases[name]; !ok {
				t.Errorf("golden %s is not in the strict-decode table", name)
			}
		}
	}
}
