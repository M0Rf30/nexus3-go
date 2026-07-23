package nexus3

import (
	"context"
	"net/http"
	"strings"

	v3 "github.com/sonatype-nexus-community/nexus-repo-api-client-go/v3"
)

// Client wraps the generated Nexus Repository Manager 3 API client with
// stored credentials and connection settings.
type Client struct {
	api      *v3.APIClient
	username string
	password string
	hasAuth  bool
}

// Option configures a Client during construction.
type Option func(*Client, *v3.Configuration)

// WithBasicAuth configures the Client to authenticate every request with the
// given username and password.
func WithBasicAuth(username, password string) Option {
	return func(c *Client, _ *v3.Configuration) {
		c.username = username
		c.password = password
		c.hasAuth = true
	}
}

// WithHTTPClient overrides the *http.Client used to perform requests.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(_ *Client, cfg *v3.Configuration) {
		cfg.HTTPClient = httpClient
	}
}

// WithUserAgent overrides the User-Agent header sent with every request.
func WithUserAgent(userAgent string) Option {
	return func(_ *Client, cfg *v3.Configuration) {
		cfg.UserAgent = userAgent
	}
}

// New creates a Client targeting the Nexus Repository Manager instance at
// baseURL, e.g. "http://localhost:8081". The "/service/rest" REST API prefix
// is appended automatically.
func New(baseURL string, opts ...Option) *Client {
	cfg := v3.NewConfiguration()
	cfg.Servers = v3.ServerConfigurations{
		{URL: strings.TrimSuffix(baseURL, "/") + "/service/rest"},
	}

	c := &Client{}
	for _, opt := range opts {
		opt(c, cfg)
	}

	c.api = v3.NewAPIClient(cfg)
	return c
}

// authCtx returns a context carrying the configured basic-auth credentials,
// as expected by every generated API call. It is a no-op passthrough when no
// credentials were configured.
func (c *Client) authCtx(ctx context.Context) context.Context {
	if !c.hasAuth {
		return ctx
	}
	return context.WithValue(ctx, v3.ContextBasicAuth, v3.BasicAuth{
		UserName: c.username,
		Password: c.password,
	})
}
