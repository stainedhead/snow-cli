// Package snfake is an httptest ServiceNow used by every stream's tests:
// fixture JSON, status/header/latency fault injection, a POST counter and
// X-Total-Count support. Frozen at A-GATE.
package snfake

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Response scripts one HTTP response.
type Response struct {
	Status  int // default 200
	JSON    any // marshalled when Body is nil
	Body    []byte
	Header  map[string]string
	Latency time.Duration
	// Total, when non-nil, is sent as X-Total-Count.
	Total *int
}

// Request is one recorded request.
type Request struct {
	Method string
	Path   string
	Query  string
	Header http.Header
	Body   []byte
}

type key struct{ method, path string }

type route struct {
	resp   Response
	fn     http.HandlerFunc
	faults []Response
}

// Fake is the fake server.
type Fake struct {
	srv *httptest.Server

	mu     sync.Mutex
	routes map[key]*route
	reqs   []Request
}

// New starts a fake and closes it with the test.
func New(t testing.TB) *Fake {
	t.Helper()
	f := &Fake{routes: map[key]*route{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

// URL is the base URL (http://127.0.0.1:port).
func (f *Fake) URL() string { return f.srv.URL }

// Host is host:port, the value for a profile's instance.host in tests.
func (f *Fake) Host() string { return f.srv.Listener.Addr().String() }

func (f *Fake) route(method, path string) *route {
	k := key{method, path}
	r := f.routes[k]
	if r == nil {
		r = &route{}
		f.routes[k] = r
	}
	return r
}

// On sets the sticky response for method+path.
func (f *Fake) On(method, path string, r Response) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rt := f.route(method, path)
	rt.resp, rt.fn = r, nil
}

// OnFunc sets a custom handler for method+path.
func (f *Fake) OnFunc(method, path string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.route(method, path).fn = h
}

// Fail makes the next times requests to method+path return r before the
// normal response resumes.
func (f *Fake) Fail(method, path string, times int, r Response) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rt := f.route(method, path)
	for i := 0; i < times; i++ {
		rt.faults = append(rt.faults, r)
	}
}

// Requests returns a copy of every recorded request in arrival order.
func (f *Fake) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.reqs...)
}

// Count counts recorded requests for method+path.
func (f *Fake) Count(method, path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.reqs {
		if r.Method == method && r.Path == path {
			n++
		}
	}
	return n
}

// Posts counts every POST received, injected faults included.
func (f *Fake) Posts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.reqs {
		if r.Method == http.MethodPost {
			n++
		}
	}
	return n
}

// Records builds a Table API list response with X-Total-Count = total.
func Records(total int, recs ...map[string]any) Response {
	if recs == nil {
		recs = []map[string]any{}
	}
	return Response{Status: 200, JSON: map[string]any{"result": recs}, Total: &total}
}

// Error builds a ServiceNow-style error response.
func Error(status int, message string) Response {
	return Response{Status: status, JSON: map[string]any{
		"error":  map[string]any{"message": message, "detail": "fake detail"},
		"status": "failure",
	}}
}

// FixtureFile loads a JSON fixture (path relative to the test's package
// directory) as a 200 response.
func FixtureFile(t testing.TB, path string) Response {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("snfake: fixture %s: %v", path, err)
	}
	return Response{Status: 200, Body: b}
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.reqs = append(f.reqs, Request{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Header: r.Header.Clone(), Body: body})
	var resp Response
	var fn http.HandlerFunc
	if rt := f.routes[key{r.Method, r.URL.Path}]; rt != nil {
		switch {
		case len(rt.faults) > 0:
			resp = rt.faults[0]
			rt.faults = rt.faults[1:]
		case rt.fn != nil:
			fn = rt.fn
		default:
			resp = rt.resp
		}
	}
	f.mu.Unlock()

	if fn != nil {
		fn(w, r)
		return
	}
	if resp.Status == 0 && resp.JSON == nil && resp.Body == nil {
		resp = Response{Status: 404, JSON: map[string]any{
			"error":  map[string]any{"message": "No Record found", "detail": "Record doesn't exist or ACL restricts the record retrieval"},
			"status": "failure",
		}}
	}
	if resp.Latency > 0 {
		time.Sleep(resp.Latency)
	}
	for k, v := range resp.Header {
		w.Header().Set(k, v)
	}
	if resp.Total != nil {
		w.Header().Set("X-Total-Count", strconv.Itoa(*resp.Total))
	}
	out := resp.Body
	if out == nil && resp.JSON != nil {
		out, _ = json.Marshal(resp.JSON)
	}
	if out != nil && w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	status := resp.Status
	if status == 0 {
		status = 200
	}
	w.WriteHeader(status)
	_, _ = w.Write(out)
}
