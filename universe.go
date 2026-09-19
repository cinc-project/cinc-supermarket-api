package supermarket

import (
	"context"
	"io"
	"net/http"
)

// Universe is the full dependency graph of every cookbook version on
// the server. It is keyed by cookbook name then by version string.
type Universe map[string]map[string]UniverseVersion

// UniverseVersion is one entry in the universe graph.
type UniverseVersion struct {
	LocationPath string            `json:"location_path"`
	LocationType string            `json:"location_type"` // "supermarket"
	Dependencies map[string]string `json:"dependencies"`
}

// UniverseService accesses the /universe endpoint.
type UniverseService struct{ client *Client }

// Get returns the entire universe in one shot. The payload can be
// tens of megabytes; prefer GetStream when memory pressure matters.
func (s *UniverseService) Get(ctx context.Context) (Universe, *Response, error) {
	return doJSON[Universe](ctx, s.client, request{
		method: http.MethodGet,
		path:   "/universe",
	})
}

// GetStream returns the universe body as a raw io.ReadCloser so the
// caller can decode it incrementally with json.Decoder. The caller
// owns closing the body.
//
// Streaming responses are not subject to the client's total-transaction
// timeout — a multi-megabyte universe would otherwise be truncated
// mid-read. Bound the download with ctx.
func (s *UniverseService) GetStream(ctx context.Context) (io.ReadCloser, *Response, error) {
	return s.client.stream(ctx, "/universe", "application/json")
}
