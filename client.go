package supermarket

import (
	"crypto/rsa"
	"crypto/tls"
	"errors"
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
	// signedClient is httpClient with a redirect guard that drops the
	// X-Ops-* credential headers when a redirect crosses to another host.
	// See newSignedClient.
	signedClient *http.Client
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
		hc, err = withInsecureTLS(hc)
		if err != nil {
			return nil, err
		}
	}
	c := &Client{
		baseURL:      base,
		username:     cfg.Username,
		key:          cfg.Key,
		httpClient:   hc,
		streamClient: newStreamClient(hc),
		signedClient: newSignedClient(hc),
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
//
// InsecureSkipVerify lives on *http.Transport's TLS config, so a caller whose
// client carries some other RoundTripper — an oauth2 wrapper, a tracing
// round-tripper, a recorder — cannot be honoured. Rather than silently
// substituting http.DefaultTransport and dropping that round-tripper on the
// floor, the combination is refused.
func withInsecureTLS(hc *http.Client) (*http.Client, error) {
	base := http.DefaultTransport.(*http.Transport)
	switch t := hc.Transport.(type) {
	case nil:
	case *http.Transport:
		base = t
	default:
		return nil, fmt.Errorf("supermarket: WithSkipTLSVerify cannot modify a %T transport; "+
			"set InsecureSkipVerify on your own *http.Transport and pass it via WithHTTPClient instead", t)
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
	}, nil
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

// maxRedirects mirrors net/http's built-in redirect cap. Installing a
// CheckRedirect function disables that default, so newSignedClient has to
// reimpose the limit itself.
const maxRedirects = 10

// newSignedClient derives the *http.Client used for signed (write) requests.
//
// net/http strips Authorization, Cookie, and WWW-Authenticate when a redirect
// crosses to a different host, but it knows nothing about Chef's X-Ops-*
// block — which is credential material of exactly the same kind. Forwarding it
// hands a Supermarket username and a complete RSA signature to a host the
// caller never addressed; against a plain-http base URL, a MITM redirect
// harvests it outright.
//
// Dropping the headers costs nothing a correct server could have used: a
// signature covers the method, path, body, and timestamp of one specific
// request, so it is meaningless at the redirect target regardless.
func newSignedClient(hc *http.Client) *http.Client {
	sc := *hc
	next := hc.CheckRedirect
	sc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			for k := range req.Header {
				if strings.HasPrefix(k, "X-Ops-") {
					req.Header.Del(k)
				}
			}
		}
		if next != nil {
			return next(req, via)
		}
		if len(via) >= maxRedirects {
			return errors.New("supermarket: stopped after 10 redirects")
		}
		return nil
	}
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
