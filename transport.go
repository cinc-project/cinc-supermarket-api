package supermarket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cinc-project/cinc-supermarket-api/internal/signing"
)

// request is the shared shape every API call funnels through.
type request struct {
	method      string
	path        string // request path (may contain query string)
	body        []byte // request body (already serialized)
	contentType string // body's Content-Type; defaults to application/json when body is non-empty
	sign        bool   // attach signed headers using c.username/c.key
	// signBody, when non-nil, is the byte slice the signer hashes for
	// X-Ops-Content-Hash instead of body. Chef's signed-header protocol
	// has a special case for multipart file uploads: the content hash
	// covers ONLY the uploaded file's bytes, not the whole multipart
	// envelope. The server (mixlib-authentication) hashes the tarball
	// file part alone, so the share upload sets this to the raw tarball
	// bytes while body stays the full multipart payload.
	signBody []byte
}

// doRaw sends req and returns the raw response body. GETs are retried
// on transient transport errors and 5xx responses.
func (c *Client) doRaw(ctx context.Context, req request) ([]byte, *Response, error) {
	if req.sign && !c.canSign() {
		return nil, nil, ErrUnauthenticatedWrite
	}
	var attempt int
	for {
		data, resp, err := c.doOnce(ctx, req)
		retriable := (resp != nil && resp.StatusCode >= 500) || isNetErr(err)
		if retriable && req.method == http.MethodGet && attempt < c.opts.maxRetries {
			attempt++
			continue
		}
		return data, resp, err
	}
}

// isNetErr reports whether err is a transient transport-level failure
// worth retrying. HTTP-status errors (*ErrorResponse) and context
// cancellation are deliberately excluded: a 4xx is a definitive answer
// and a cancelled context means the caller has given up. 5xx responses
// are handled separately by the status check in doRaw.
func isNetErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var er *ErrorResponse
	return !errors.As(err, &er)
}

func (c *Client) doOnce(ctx context.Context, r request) ([]byte, *Response, error) {
	u := c.baseURL.String() + r.path
	var rdr io.Reader
	if len(r.body) > 0 {
		rdr = bytes.NewReader(r.body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, r.method, u, rdr)
	if err != nil {
		return nil, nil, fmt.Errorf("supermarket: build request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", c.opts.userAgent)
	if len(r.body) > 0 {
		ct := r.contentType
		if ct == "" {
			ct = "application/json"
		}
		httpReq.Header.Set("Content-Type", ct)
	}
	if r.sign {
		signPath := r.path
		if i := strings.IndexByte(signPath, '?'); i >= 0 {
			signPath = signPath[:i]
		}
		signBody := r.body
		if r.signBody != nil {
			signBody = r.signBody
		}
		hdrs, err := signing.SignHeaders(signing.Request{
			Method: r.method, Path: signPath, Body: signBody,
			UserID: c.username, Timestamp: c.timestamp(),
		}, c.key)
		if err != nil {
			return nil, nil, err
		}
		for k, v := range hdrs {
			httpReq.Header[k] = v
		}
	}

	// Signed requests go through the client that drops the X-Ops-*
	// credential headers if a redirect crosses to another host.
	client := c.httpClient
	if r.sign {
		client = c.signedClient
	}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("supermarket: %s %s: %w", r.method, r.path, err)
	}
	defer httpResp.Body.Close()
	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("supermarket: read body: %w", err)
	}
	resp := &Response{HTTPResponse: httpResp, StatusCode: httpResp.StatusCode}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return data, resp, newErrorResponse(r.method, r.path, httpResp.StatusCode, data)
	}
	return data, resp, nil
}

// doJSON sends a request expecting a JSON 2xx response decoded into T.
func doJSON[T any](ctx context.Context, c *Client, r request) (T, *Response, error) {
	var zero T
	data, resp, err := c.doRaw(ctx, r)
	if err != nil {
		return zero, resp, err
	}
	if len(data) == 0 {
		return zero, resp, nil
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return zero, resp, fmt.Errorf("supermarket: decode response: %w", err)
	}
	return out, resp, nil
}

// stream sends a GET and returns the response body as an io.ReadCloser
// for callers that want to decode lazily (e.g. /universe or cookbook
// tarball downloads). The caller is responsible for closing the body.
//
// It deliberately uses c.streamClient rather than c.httpClient: the latter
// carries a total-transaction Timeout that would truncate a large body
// mid-read. Bound a stream with ctx instead.
func (c *Client) stream(ctx context.Context, path string) (io.ReadCloser, *Response, error) {
	u := c.baseURL.String() + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("supermarket: build request: %w", err)
	}
	httpReq.Header.Set("User-Agent", c.opts.userAgent)
	httpResp, err := c.streamClient.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("supermarket: GET %s: %w", path, err)
	}
	resp := &Response{HTTPResponse: httpResp, StatusCode: httpResp.StatusCode}
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		data, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return nil, resp, newErrorResponse(http.MethodGet, path, httpResp.StatusCode, data)
	}
	return httpResp.Body, resp, nil
}
