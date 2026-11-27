package supermarket

import "net/url"

// Page is the envelope every Supermarket list/search response uses.
// Items carries the actual result rows; Start is the offset of the first.
//
// Total is only as good as the server makes it. Supermarket reports the
// number of cookbooks on the whole server for Search.Cookbooks and for a
// Cookbooks.List filtered by User or Platforms, and the size of the
// returned page for Tools.Search. Only an unfiltered Cookbooks.List or
// Tools.List reports a true total. A page with no items is the reliable
// end-of-results signal.
type Page[T any] struct {
	Start int `json:"start"`
	Total int `json:"total"`
	Items []T `json:"items"`
}

// HasMore reports whether the server has more results past this page.
// A page that carries no items is always terminal, even if Total claims
// otherwise — otherwise NextStart would not advance and a caller's
// pagination loop would spin forever.
func (p Page[T]) HasMore() bool {
	return len(p.Items) > 0 && p.Start+len(p.Items) < p.Total
}

// NextStart returns the Start offset a caller should pass to fetch the
// page that immediately follows p. Returns 0 when HasMore is false.
func (p Page[T]) NextStart() int {
	if !p.HasMore() {
		return 0
	}
	return p.Start + len(p.Items)
}

// applyPageQuery sets start/items query params when non-zero.
func applyPageQuery(q url.Values, start, items int) {
	if start > 0 {
		q.Set("start", itoa(start))
	}
	if items > 0 {
		q.Set("items", itoa(items))
	}
}
