package supermarket

import "net/http"

// Response wraps the raw HTTP response returned by an API call.
//
// For the decoding calls (everything returning a parsed type) the body has
// already been consumed and closed by the time the caller sees the Response,
// so HTTPResponse is useful only for its status and headers.
//
// The streaming calls — Universe.GetStream and Cookbooks.Download — are the
// exception: they hand back the still-open body as their first return value,
// and HTTPResponse.Body is that same ReadCloser. Closing it is the caller's
// job either way.
type Response struct {
	HTTPResponse *http.Response
	StatusCode   int
}
