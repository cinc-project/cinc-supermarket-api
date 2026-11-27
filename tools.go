package supermarket

import (
	"context"
	"net/http"
	"net/url"
)

// Tool is the full record returned by GET /tools/:slug.
type Tool struct {
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	Type          string `json:"type"`
	SourceURL     string `json:"source_url"`
	Description   string `json:"description"`
	Instructions  string `json:"instructions"` // markdown
	Owner         string `json:"owner"`
	UpForAdoption bool   `json:"up_for_adoption"`
}

// ToolSummary is the lighter shape returned by list and search.
type ToolSummary struct {
	Name        string `json:"tool_name"`
	Type        string `json:"tool_type"`
	SourceURL   string `json:"tool_source_url"`
	Description string `json:"tool_description"`
	Owner       string `json:"tool_owner"`
	URL         string `json:"tool"`
}

// ToolsListOptions configures GET /tools.
type ToolsListOptions struct {
	Start int
	Items int
	// Order, when set, is ToolOrderRecentlyAdded.
	Order string
}

// Tool types, for ToolSearchOptions.Type. These are the values Supermarket
// accepts; it ignores any other.
const (
	ToolTypeKnifePlugin       = "knife_plugin"
	ToolTypeOhaiPlugin        = "ohai_plugin"
	ToolTypeChefInfraTool     = "chef_infra_tool"
	ToolTypeChefInfraHandler  = "chef_infra_handler"
	ToolTypeKitchenDriver     = "kitchen_driver"
	ToolTypePowerShellModule  = "powershell_module"
	ToolTypeDSCResource       = "dsc_resource"
	ToolTypeComplianceProfile = "compliance_profile"
)

// ToolOrderRecentlyAdded is the only tool ordering Supermarket supports
// besides the default (by name).
const ToolOrderRecentlyAdded = "recently_added"

// ToolSearchOptions configures GET /tools-search.
type ToolSearchOptions struct {
	Q     string // query; empty matches every tool
	Start int
	Items int
	Type  string // one of the ToolType* constants; empty means any type
	Order string // ToolOrderRecentlyAdded, or empty for by name
}

// ToolsService accesses the /tools endpoints.
type ToolsService struct{ client *Client }

// List paginates the tools index.
func (s *ToolsService) List(ctx context.Context, opts ToolsListOptions) (Page[ToolSummary], *Response, error) {
	q := url.Values{}
	applyPageQuery(q, opts.Start, opts.Items)
	if opts.Order != "" {
		q.Set("order", opts.Order)
	}
	return doJSON[Page[ToolSummary]](ctx, s.client, request{
		method: http.MethodGet,
		path:   withQuery("/api/v1/tools", q),
	})
}

// Get returns a single tool by slug.
func (s *ToolsService) Get(ctx context.Context, slug string) (*Tool, *Response, error) {
	if err := requireArg("tool slug", slug); err != nil {
		return nil, nil, err
	}
	t, resp, err := doJSON[Tool](ctx, s.client, request{
		method: http.MethodGet,
		path:   "/api/v1/tools/" + url.PathEscape(slug),
	})
	if err != nil {
		return nil, resp, err
	}
	return &t, resp, nil
}

// Search runs a fuzzy search over the tools index.
//
// Supermarket reports Total for this endpoint as the number of items in the
// returned page, not the number of matches, so Page.HasMore cannot tell
// whether more pages exist. Request the next Start until a page comes back
// empty.
func (s *ToolsService) Search(ctx context.Context, opts ToolSearchOptions) (Page[ToolSummary], *Response, error) {
	q := url.Values{}
	q.Set("q", opts.Q)
	applyPageQuery(q, opts.Start, opts.Items)
	if opts.Type != "" {
		q.Set("type", opts.Type)
	}
	if opts.Order != "" {
		q.Set("order", opts.Order)
	}
	return doJSON[Page[ToolSummary]](ctx, s.client, request{
		method: http.MethodGet,
		path:   withQuery("/api/v1/tools-search", q),
	})
}
