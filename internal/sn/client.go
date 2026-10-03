// Package sn is the ServiceNow HTTP adapter: a thin client over the core
// httpx transport (host allowlist, single 401 refresh, bounded retries) that
// maps responses to output.CategoryError types (spec D-b). Other streams add
// endpoint files (tables_*.go, writes_*.go) on top of Do.
package sn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/stainedhead/agent-cli-core/httpx"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/config"
)

// maxBody bounds how much of a response is read.
const maxBody = 8 << 20

// maxMessage bounds error text taken from a ServiceNow error body.
const maxMessage = 300

// Options configures a Client.
type Options struct {
	// Host is the validated instance host (host or host:port). It is the only
	// host the client will contact.
	Host string
	// Insecure selects http; permitted only for loopback hosts (httptest).
	Insecure bool
	// Auth authorizes requests and refreshes on 401 (an *auth.Authorizer).
	Auth httpx.TokenRefresher
	// Base is the underlying round tripper; nil means http.DefaultTransport.
	Base http.RoundTripper
	// Transport carries retry/trace tuning. AllowedHosts, Refresher and
	// AllowInsecureHTTP are always overridden by the client.
	Transport httpx.Config
}

// Client talks to one ServiceNow instance.
type Client struct {
	base   url.URL
	client *http.Client
}

// New validates options and builds a Client.
func New(o Options) (*Client, error) {
	host, err := config.ValidateHost(o.Host)
	if err != nil {
		return nil, err
	}
	if o.Auth == nil {
		return nil, errors.New("sn: an authorizer is required")
	}
	scheme := "https"
	if o.Insecure {
		if !isLoopback(host) {
			return nil, fmt.Errorf("sn: http is only permitted for loopback hosts, not %q", host)
		}
		scheme = "http"
	}
	cfg := o.Transport
	cfg.AllowedHosts = []string{host}
	cfg.Refresher = o.Auth
	cfg.AllowInsecureHTTP = false
	tr := httpx.NewTransport(o.Base, cfg)
	cl := httpx.NewClient(cfg)
	cl.Transport = tr
	return &Client{base: url.URL{Scheme: scheme, Host: host}, client: cl}, nil
}

func isLoopback(hostport string) bool {
	h := hostport
	if hh, _, err := net.SplitHostPort(hostport); err == nil {
		h = hh
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// Call is one request.
type Call struct {
	Method string
	Path   string // absolute, beginning /api/
	Query  url.Values
	Body   any // JSON-marshalled when non-nil
	// SafeToRetry marks a non-idempotent request retryable (D-c). Set it only
	// after the dedupe query ran and found nothing.
	SafeToRetry bool
}

// Response is a successful (2xx) response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
	// Total is X-Total-Count, nil when absent or unparseable.
	Total *int
}

// Do sends the request and maps non-2xx responses to typed errors.
func (c *Client) Do(ctx context.Context, call Call) (*Response, error) {
	if !strings.HasPrefix(call.Path, "/api/") || strings.HasPrefix(call.Path, "//") || strings.ContainsAny(call.Path, "?#") {
		return nil, fmt.Errorf("sn: refusing path %q: must be an absolute /api/ path", call.Path)
	}
	u := c.base
	u.Path = call.Path
	u.RawQuery = call.Query.Encode()

	var body io.Reader
	if call.Body != nil {
		b, err := json.Marshal(call.Body)
		if err != nil {
			return nil, fmt.Errorf("sn: encode body: %w", err)
		}
		body = bytes.NewReader(b)
	}
	method := call.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("sn: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if call.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if call.SafeToRetry {
		req = httpx.MarkSafeToRetry(req)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, unwrapURLError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("sn: read response: %w", err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		r := &Response{Status: resp.StatusCode, Header: resp.Header, Body: data}
		if n, err := strconv.Atoi(resp.Header.Get("X-Total-Count")); err == nil && n >= 0 {
			r.Total = &n
		}
		return r, nil
	}
	return nil, mapStatus(resp.StatusCode, data)
}

// unwrapURLError surfaces the typed core error that net/http wrapped in a
// *url.Error, so output.CategoryOf finds it.
func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		var ce output.CategoryError
		if errors.As(ue.Err, &ce) {
			return ue.Err
		}
	}
	return err
}

func mapStatus(status int, body []byte) error {
	msg := errorMessage(body)
	switch {
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		return &ValidationError{Status: status, Message: msg}
	case status == http.StatusNotFound:
		return &NotFoundError{Status: status, Message: msg}
	case status == http.StatusConflict || status == http.StatusPreconditionFailed:
		return &ConflictError{Status: status, Message: msg}
	case status >= 500:
		// Statuses the core does not already classify (500, 501, 505...).
		return &httpx.RateLimitedError{Status: status, Attempts: 1}
	}
	return &APIError{Status: status, Message: msg}
}

// errorMessage extracts and scrubs error.message/detail from a ServiceNow
// error body. Non-JSON bodies are never echoed.
func errorMessage(body []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
			Detail  string `json:"detail"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) != nil || (e.Error.Message == "" && e.Error.Detail == "") {
		return "no error message in response"
	}
	msg := e.Error.Message
	if e.Error.Detail != "" && e.Error.Detail != msg {
		msg += " (" + e.Error.Detail + ")"
	}
	msg = output.Failure(output.CategoryGeneral, msg, "").Error.Message
	if len(msg) > maxMessage {
		msg = strings.ToValidUTF8(msg[:maxMessage], "") + "..."
	}
	return msg
}
