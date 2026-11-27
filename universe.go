package supermarket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
)

// Universe is the full dependency graph of every cookbook version on
// the server. It is keyed by cookbook name then by version string.
type Universe map[string]map[string]UniverseVersion

// UniverseVersion is one entry in the universe graph.
type UniverseVersion struct {
	// LocationType is "opscode": Supermarket keeps the legacy value because
	// Berkshelf matches on it.
	LocationType string `json:"location_type"`
	// LocationPath is the API base URL the version is served from.
	LocationPath string `json:"location_path"`
	// DownloadURL is the URL of this version's tarball.
	DownloadURL  string            `json:"download_url"`
	Dependencies map[string]string `json:"dependencies"`
}

// UniverseService accesses the /universe endpoint.
type UniverseService struct{ client *Client }

// Get returns the entire universe in one shot. The payload can be
// tens of megabytes; prefer Entries when memory pressure matters.
func (s *UniverseService) Get(ctx context.Context) (Universe, *Response, error) {
	return doJSON[Universe](ctx, s.client, request{
		method: http.MethodGet,
		path:   "/universe",
	})
}

// GetStream returns the universe body as a raw io.ReadCloser so the
// caller can decode it incrementally with json.Decoder. The caller
// owns closing the body. Most callers want Entries, which does that
// decoding and yields one UniverseEntry per cookbook version.
//
// Streaming responses are not subject to the client's total-transaction
// timeout — a multi-megabyte universe would otherwise be truncated
// mid-read. Bound the download with ctx.
func (s *UniverseService) GetStream(ctx context.Context) (io.ReadCloser, *Response, error) {
	return s.client.stream(ctx, "/universe", "application/json")
}

// UniverseEntry is one cookbook version from the universe.
type UniverseEntry struct {
	Cookbook string
	Version  string
	UniverseVersion
}

// Entries decodes the universe incrementally and yields one entry per
// cookbook version, so memory stays flat however large the document is.
// The request is sent when iteration starts, not when Entries is called.
//
// It is built on GetStream: there is no total-transaction timeout, so bound
// the download with ctx. A request error, an error response, a malformed or
// truncated document, or ctx being cancelled mid-stream yields a single
// (zero, err) and ends the iteration; a clean end means the whole document
// was read. The response body is closed when iteration ends, including when
// the caller breaks out of the loop early.
func (s *UniverseService) Entries(ctx context.Context) iter.Seq2[UniverseEntry, error] {
	return func(yield func(UniverseEntry, error) bool) {
		body, _, err := s.GetStream(ctx)
		if err != nil {
			yield(UniverseEntry{}, err)
			return
		}
		defer body.Close()
		if err := decodeUniverse(ctx, json.NewDecoder(body), yield); err != nil {
			yield(UniverseEntry{}, err)
		}
	}
}

// decodeUniverse walks the two outer object levels with Token and decodes
// each version value on its own. It returns nil on a complete document or
// an early stop, and the error to yield otherwise.
func decodeUniverse(ctx context.Context, dec *json.Decoder, yield func(UniverseEntry, error) bool) error {
	fail := func(err error) error {
		// A read that failed because ctx was cancelled shows up as a network
		// or EOF error from the body; report the cancellation itself.
		if cerr := ctx.Err(); cerr != nil {
			return fmt.Errorf("supermarket: GET /universe: %w", cerr)
		}
		// Token reports a document that ends early as plain io.EOF.
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return fmt.Errorf("supermarket: decode universe: %w", err)
	}
	if err := expectDelim(dec, '{'); err != nil {
		return fail(err)
	}
	for dec.More() {
		cookbook, err := stringToken(dec)
		if err != nil {
			return fail(err)
		}
		if err := expectDelim(dec, '{'); err != nil {
			return fail(fmt.Errorf("cookbook %q: %w", cookbook, err))
		}
		for dec.More() {
			version, err := stringToken(dec)
			if err != nil {
				return fail(err)
			}
			e := UniverseEntry{Cookbook: cookbook, Version: version}
			if err := dec.Decode(&e.UniverseVersion); err != nil {
				return fail(fmt.Errorf("%s %s: %w", cookbook, version, err))
			}
			// Check between entries as well as on read errors: the decoder
			// may already hold the rest of the document in its buffer, and a
			// cancelled caller must not see that as a complete universe.
			if err := ctx.Err(); err != nil {
				return fail(err)
			}
			if !yield(e, nil) {
				return nil
			}
		}
		if err := expectDelim(dec, '}'); err != nil {
			return fail(err)
		}
	}
	if err := expectDelim(dec, '}'); err != nil {
		return fail(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("unexpected data after the universe object")
		}
		return fail(err)
	}
	return nil
}

// expectDelim reads the next token and requires it to be want.
func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != want {
		return fmt.Errorf("expected %q, got %v", want, tok)
	}
	return nil
}

// stringToken reads an object key.
func stringToken(dec *json.Decoder) (string, error) {
	tok, err := dec.Token()
	if err != nil {
		return "", err
	}
	s, ok := tok.(string)
	if !ok {
		return "", fmt.Errorf("expected object key, got %v", tok)
	}
	return s, nil
}
