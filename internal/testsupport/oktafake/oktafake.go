// Package oktafake is a skeleton httptest Okta authorization server for
// human-mode tests (WS-D extends it by adding files, not by editing this
// one). It scripts responses per endpoint and records form requests.
package oktafake

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

// Request is one recorded request.
type Request struct {
	Method string
	Path   string
	Form   url.Values
	Header http.Header
}

type scripted struct {
	status int
	body   string
}

// Fake is the fake Okta server. Endpoints (relative to Issuer):
// /v1/token, /v1/device/authorize, /v1/revoke.
type Fake struct {
	srv *httptest.Server

	mu       sync.Mutex
	tokens   []scripted
	devices  []scripted
	revoke   int
	requests []Request
}

// New starts the fake and closes it with the test.
func New(t testing.TB) *Fake {
	t.Helper()
	f := &Fake{revoke: http.StatusOK}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

// Issuer is the base URL used as the OAuth issuer.
func (f *Fake) Issuer() string { return f.srv.URL }

// Host is host:port.
func (f *Fake) Host() string { return f.srv.Listener.Addr().String() }

// QueueToken scripts the next /v1/token response.
func (f *Fake) QueueToken(status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens = append(f.tokens, scripted{status, body})
}

// QueueDevice scripts the next /v1/device/authorize response.
func (f *Fake) QueueDevice(status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.devices = append(f.devices, scripted{status, body})
}

// SetRevoke sets the status of /v1/revoke.
func (f *Fake) SetRevoke(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoke = status
}

// Requests returns the recorded requests.
func (f *Fake) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.requests...)
}

// Count counts recorded requests for path.
func (f *Fake) Count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if r.Path == path {
			n++
		}
	}
	return n
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	f.mu.Lock()
	f.requests = append(f.requests, Request{Method: r.Method, Path: r.URL.Path, Form: r.PostForm, Header: r.Header.Clone()})
	var s scripted
	switch r.URL.Path {
	case "/v1/token":
		s, f.tokens = pop(f.tokens)
	case "/v1/device/authorize":
		s, f.devices = pop(f.devices)
	case "/v1/revoke":
		s = scripted{status: f.revoke, body: "{}"}
	default:
		s = scripted{status: http.StatusNotFound, body: `{"error":"not_found"}`}
	}
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(s.status)
	_, _ = w.Write([]byte(s.body))
}

func pop(q []scripted) (scripted, []scripted) {
	if len(q) == 0 {
		return scripted{status: http.StatusInternalServerError, body: `{"error":"oktafake: no scripted response"}`}, q
	}
	return q[0], q[1:]
}
