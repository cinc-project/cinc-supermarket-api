package supermarket

import (
	"crypto/rsa"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a Chef Supermarket API client. It is safe for concurrent use.
//
// A Client with no Username/Key may call only the anonymous read
// endpoints; write endpoints (share, delete) return
// ErrUnauthenticatedWrite without contacting the server.
type Client struct {
	baseURL    *url.URL
	username   string
	key        *rsa.PrivateKey
	httpClient *http.Client
	// streamClient is httpClient without a total-transaction deadline; it
	// serves the endpoints whose body is read incrementally. See
	// newStreamClient.
	streamClient *http.Client
	opts         options
	clock        func() time.Time

	// Services.
	Cookbooks *CookbooksService
	Search    *SearchService
	Tools     *ToolsService
	Users     *UsersService
	Universe  *UniverseService
	Health    *HealthService
}

// NewClient builds a Client from cfg and optional Options.
func NewClient(cfg Config, opts ...Option) (*Client, error) {
	raw := cfg.BaseURL
	if raw == "" {
		raw = DefaultBaseURL
	}
	base, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || base.Host == "" {
		return nil, fmt.Errorf("supermarket: invalid BaseURL %q", raw)
	}
	// url.Parse is permissive; net/http is not. Reject what it can never
	// fetch at construction rather than once per call.
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("supermarket: BaseURL %q has scheme %q, want http or https", raw, base.Scheme)
	}
	// Request paths are appended by concatenation, so a query or fragment on
	// the base would land in the middle of every URL: "http://x?a=1" plus
	// "/api/v1/cookbooks" yields "http://x?a=1/api/v1/cookbooks".
	if base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("supermarket: BaseURL %q must not carry a query or fragment", raw)
	}
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}
	hc := o.httpClient
	if o.skipTLSVerify {
		hc = withInsecureTLS(hc)
	}
	c := &Client{
		baseURL:      base,
		username:     cfg.Username,
		key:          cfg.Key,
		httpClient:   hc,
		streamClient: newStreamClient(hc),
		opts:         o,
		clock:        time.Now,
	}
	c.Cookbooks = &CookbooksService{client: c}
	c.Search = &SearchService{client: c}
	c.Tools = &ToolsService{client: c}
	c.Users = &UsersService{client: c}
	c.Universe = &UniverseService{client: c}
	c.Health = &HealthService{client: c}
	return c, nil
}

// withInsecureTLS returns a copy of hc whose transport skips TLS
// certificate verification, preserving the caller's other client and
// transport settings (Timeout, CheckRedirect, Jar, connection-pool
// tuning) rather than discarding a custom *http.Client. Only the TLS
// verification flag is changed.
func withInsecureTLS(hc *http.Client) *http.Client {
	base := http.DefaultTransport.(*http.Transport)
	if t, ok := hc.Transport.(*http.Transport); ok {
		base = t
	}
	tr := base.Clone()
	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{}
	}
	tr.TLSClientConfig.InsecureSkipVerify = true
	return &http.Client{
		Transport:     tr,
		Timeout:       hc.Timeout,
		CheckRedirect: hc.CheckRedirect,
		Jar:           hc.Jar,
	}
}

// defaultStreamHeaderTimeout bounds how long a streaming request waits for
// response headers when the caller's client has no total timeout of its own
// to borrow the value from.
const defaultStreamHeaderTimeout = 60 * time.Second

// newStreamClient derives the *http.Client used for responses whose body the
// caller reads incrementally (/universe, cookbook tarball downloads) from hc.
//
// http.Client.Timeout is a whole-transaction deadline: the timer keeps running
// after Do returns and interrupts reading of Response.Body. That makes it the
// wrong tool for the streaming endpoints, where the body is the large part —
// the 60s default silently truncates a multi-megabyte /universe on a slow
// link, and the caller sees a confusing mid-read timeout instead of data.
//
// So the streaming client drops Timeout and moves the bound onto the phases
// that genuinely should be bounded: connect and response-header wait, both on
// the transport. The caller's context remains the brake on the overall
// operation and still aborts an in-flight read.
func newStreamClient(hc *http.Client) *http.Client {
	sc := *hc
	sc.Timeout = 0

	headerTimeout := hc.Timeout
	if headerTimeout <= 0 {
		headerTimeout = defaultStreamHeaderTimeout
	}
	base, ok := hc.Transport.(*http.Transport)
	if !ok {
		// A custom RoundTripper owns its own timeouts; leave it alone and
		// settle for having removed the total-transaction deadline.
		if hc.Transport != nil {
			return &sc
		}
		base = http.DefaultTransport.(*http.Transport)
	}
	tr := base.Clone()
	if tr.ResponseHeaderTimeout == 0 {
		tr.ResponseHeaderTimeout = headerTimeout
	}
	sc.Transport = tr
	return &sc
}

// canSign reports whether the client carries credentials for the
// signed-header protocol.
func (c *Client) canSign() bool {
	return c.username != "" && c.key != nil
}

// timestamp returns the current time as an ISO-8601 UTC string.
func (c *Client) timestamp() string {
	return c.clock().UTC().Format("2006-01-02T15:04:05Z")
}
