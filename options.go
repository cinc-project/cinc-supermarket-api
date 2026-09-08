package supermarket

import (
	"crypto/rsa"
	"net/http"
	"time"
)

// DefaultBaseURL is the public Supermarket instance hosted by Progress.
const DefaultBaseURL = "https://supermarket.chef.io"

// Config holds the parameters for constructing a Client. Username and
// Key are only needed for the write endpoints (share / delete); read
// endpoints work with an empty Config.
type Config struct {
	BaseURL  string          // http or https URL; defaults to DefaultBaseURL
	Username string          // Supermarket username (write endpoints only)
	Key      *rsa.PrivateKey // RSA private key registered on the user's profile (write endpoints only)
}

type options struct {
	httpClient    *http.Client
	userAgent     string
	skipTLSVerify bool
	maxRetries    int
}

func defaultOptions() options {
	return options{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		userAgent:  "cinc-supermarket-go",
		maxRetries: 2,
	}
}

// Option customizes a Client.
type Option func(*options)

// WithHTTPClient sets a custom *http.Client. A nil client is ignored.
func WithHTTPClient(c *http.Client) Option {
	return func(o *options) {
		if c != nil {
			o.httpClient = c
		}
	}
}

// WithUserAgent sets the User-Agent header.
func WithUserAgent(ua string) Option { return func(o *options) { o.userAgent = ua } }

// WithSkipTLSVerify disables TLS certificate verification (testing only).
func WithSkipTLSVerify(skip bool) Option {
	return func(o *options) { o.skipTLSVerify = skip }
}

// WithMaxRetries sets the retry count for idempotent (GET) requests.
func WithMaxRetries(n int) Option {
	return func(o *options) {
		if n >= 0 {
			o.maxRetries = n
		}
	}
}
