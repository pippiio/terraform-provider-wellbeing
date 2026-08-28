package wellbeingclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
)

const (
	// DefaultHost is the Wellbeing production API endpoint.
	DefaultHost = "https://api-ne1.worklifebarometer.com/"

	defaultTimeout      = 5 * time.Minute
	defaultMaxRetries   = 3
	defaultPollInitial  = 5 * time.Second
	defaultPollMaximum  = 30 * time.Second
	defaultUserAgentTag = "terraform-provider-wellbeing"
)

// Client talks to the Wellbeing API for a single company.
type Client struct {
	httpClient *retryablehttp.Client
	host       string
	token      string
	companyID  string
	userAgent  string

	pollInitial time.Duration
	pollMaximum time.Duration
}

// Option customises a Client at construction time.
type Option func(*Client)

// WithHTTPClient replaces the underlying HTTP client. Intended for tests.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient.HTTPClient = h }
}

// WithTimeout sets the per-request timeout. A full roster PUT carrying
// thousands of employees needs considerably more than a default of a few
// seconds, so this defaults to five minutes.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.httpClient.HTTPClient.Timeout = d }
}

// WithMaxRetries sets how many times a retryable failure is re-attempted.
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.httpClient.RetryMax = n }
}

// WithUserAgent overrides the User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua }
}

// WithPollIntervals sets the backoff bounds used by WaitForOperation.
func WithPollIntervals(initial, maximum time.Duration) Option {
	return func(c *Client) {
		c.pollInitial = initial
		c.pollMaximum = maximum
	}
}

// NewClient builds a Client. host may include a trailing slash.
func NewClient(host, token, companyID string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(host) == "" {
		return nil, errors.New("wellbeing: host is required")
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("wellbeing: token is required")
	}
	if strings.TrimSpace(companyID) == "" {
		return nil, errors.New("wellbeing: company id is required")
	}

	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = defaultMaxRetries
	retryClient.HTTPClient.Timeout = defaultTimeout
	// retryablehttp logs every attempt to stderr by default, which corrupts the
	// go-plugin protocol stream. Terraform logging goes through tflog instead.
	retryClient.Logger = nil

	// Once retries are exhausted retryablehttp discards the final response and
	// returns an error of its own, which loses the API's explanation. The survey
	// endpoints report validation failures as a 500 carrying the reason in the
	// body ("Survey must contain one cover page and one thank you page"), so
	// hand the last response back and let do parse it into an APIError.
	retryClient.ErrorHandler = func(resp *http.Response, err error, _ int) (*http.Response, error) {
		if resp != nil {
			return resp, nil
		}
		return nil, err
	}

	c := &Client{
		httpClient:  retryClient,
		host:        strings.TrimSuffix(strings.TrimSpace(host), "/"),
		token:       strings.TrimSpace(token),
		companyID:   strings.TrimSpace(companyID),
		userAgent:   defaultUserAgentTag,
		pollInitial: defaultPollInitial,
		pollMaximum: defaultPollMaximum,
	}

	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// CompanyID returns the company this client is scoped to.
func (c *Client) CompanyID() string { return c.companyID }

// do performs a request against path, encoding in as JSON when non-nil and
// decoding the response into out when non-nil. It returns the HTTP status code
// so callers can distinguish a synchronous 200 from a queued 202.
func (c *Client) do(ctx context.Context, method, path string, in, out any) (int, error) {
	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return 0, fmt.Errorf("wellbeing: encoding request body: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := retryablehttp.NewRequestWithContext(ctx, method, c.host+path, body)
	if err != nil {
		return 0, fmt.Errorf("wellbeing: building request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("wellbeing: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, fmt.Errorf("wellbeing: reading response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, parseAPIError(resp.StatusCode, raw)
	}

	if out != nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("wellbeing: decoding response from %s %s: %w", method, path, err)
		}
	}

	return resp.StatusCode, nil
}
