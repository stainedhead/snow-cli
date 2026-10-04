package sn_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/httpx"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/sn"
	"github.com/stainedhead/snow-cli/internal/testsupport/snfake"
)

func newClient(t *testing.T, f *snfake.Fake, scenario authtest.Scenario) *sn.Client {
	t.Helper()
	fk := authtest.New(scenario)
	src, err := auth.NewDaemonTokenSource(fk, "snow")
	if err != nil {
		t.Fatal(err)
	}
	c, err := sn.New(sn.Options{
		Host: f.Host(), Insecure: true, Auth: auth.NewAuthorizer(src),
		Transport: httpx.Config{BaseDelay: 1, MaxDelay: 2, Jitter: -1},
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func get(c *sn.Client, path string) (*sn.Response, error) {
	return c.Do(context.Background(), sn.Call{Method: "GET", Path: path})
}

// Every status row of spec D-b.
func TestStatusMap(t *testing.T) {
	tests := []struct {
		status   int
		wantExit output.ExitCode
		wantCat  output.Category
		typed    func(error) bool
	}{
		{400, output.ExitValidation, output.CategoryValidation, func(e error) bool { var x *sn.ValidationError; return errors.As(e, &x) }},
		{422, output.ExitValidation, output.CategoryValidation, func(e error) bool { var x *sn.ValidationError; return errors.As(e, &x) }},
		{401, output.ExitAuth, output.CategoryAuth, func(e error) bool { var x *httpx.AuthError; return errors.As(e, &x) }},
		{403, output.ExitForbidden, output.CategoryForbidden, func(e error) bool { var x *httpx.ForbiddenError; return errors.As(e, &x) }},
		{404, output.ExitNotFound, output.CategoryNotFound, func(e error) bool { var x *sn.NotFoundError; return errors.As(e, &x) }},
		{409, output.ExitConflict, output.CategoryConflict, func(e error) bool { var x *sn.ConflictError; return errors.As(e, &x) }},
		{412, output.ExitConflict, output.CategoryConflict, func(e error) bool { var x *sn.ConflictError; return errors.As(e, &x) }},
		{429, output.ExitRateLimited, output.CategoryRateLimited, func(e error) bool { var x *httpx.RateLimitedError; return errors.As(e, &x) }},
		{500, output.ExitRateLimited, output.CategoryRateLimited, func(e error) bool { var x *sn.ServerError; return errors.As(e, &x) }},
		{501, output.ExitRateLimited, output.CategoryRateLimited, func(e error) bool { var x *sn.ServerError; return errors.As(e, &x) }},
		{505, output.ExitRateLimited, output.CategoryRateLimited, func(e error) bool { var x *sn.ServerError; return errors.As(e, &x) }},
		{503, output.ExitRateLimited, output.CategoryRateLimited, func(e error) bool { var x *httpx.RateLimitedError; return errors.As(e, &x) }},
		{418, output.ExitGeneral, output.CategoryGeneral, func(e error) bool { var x *sn.APIError; return errors.As(e, &x) }},
		{405, output.ExitGeneral, output.CategoryGeneral, func(e error) bool { var x *sn.APIError; return errors.As(e, &x) }},
	}
	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			f := snfake.New(t)
			f.On("GET", "/api/now/v1/table/incident", snfake.Error(tc.status, "boom message"))
			c := newClient(t, f, authtest.Valid)
			_, err := get(c, "/api/now/v1/table/incident")
			if err == nil {
				t.Fatal("expected error")
			}
			if output.ExitOf(err) != tc.wantExit || output.CategoryOf(err) != tc.wantCat {
				t.Errorf("exit=%d cat=%s, want %d %s (%v)", output.ExitOf(err), output.CategoryOf(err), tc.wantExit, tc.wantCat, err)
			}
			if !tc.typed(err) {
				t.Errorf("wrong error type %T", err)
			}
		})
	}
}

func TestPassthroughOfCoreErrorsIsUnmapped(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", "/api/now/v1/p", snfake.Error(403, "x"))
	c := newClient(t, f, authtest.Valid)
	_, err := get(c, "/api/now/v1/p")
	var fe *httpx.ForbiddenError
	if !errors.As(err, &fe) {
		t.Fatalf("403 must reach the caller as the core's ForbiddenError, got %T", err)
	}
}

func TestSNErrorMessageCarriedAndBounded(t *testing.T) {
	f := snfake.New(t)
	f.On("POST", "/api/now/v1/v", snfake.Error(400, "Mandatory field short_description missing"))
	c := newClient(t, f, authtest.Valid)
	_, err := c.Do(context.Background(), sn.Call{Method: "POST", Path: "/api/now/v1/v", Body: map[string]string{"a": "b"}})
	if err == nil || !strings.Contains(err.Error(), "Mandatory field short_description missing") {
		t.Fatalf("err = %v", err)
	}
	f.On("GET", "/api/now/v1/long", snfake.Error(404, strings.Repeat("x", 5000)))
	_, err = get(c, "/api/now/v1/long")
	if err == nil || len(err.Error()) > 700 {
		t.Errorf("message not bounded: %d", len(err.Error()))
	}
	// hostile body that is not JSON must not be echoed
	f.On("GET", "/api/now/v1/html", snfake.Response{Status: 404, Body: []byte("<html>secret internal page</html>")})
	_, err = get(c, "/api/now/v1/html")
	if err == nil || strings.Contains(err.Error(), "secret internal page") {
		t.Errorf("non-JSON error bodies must not be echoed: %v", err)
	}
}

func TestSNErrorMessageIsScrubbed(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", "/api/now/v1/s", snfake.Error(400, "bad Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGH"))
	c := newClient(t, f, authtest.Valid)
	_, err := get(c, "/api/now/v1/s")
	if err == nil || strings.Contains(err.Error(), "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGH") {
		t.Errorf("credential leaked in message: %v", err)
	}
}

func TestSuccessParsesTotalAndBody(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", "/api/now/v1/table/incident", snfake.Records(42, map[string]any{"number": "INC1"}))
	c := newClient(t, f, authtest.Valid)
	r, err := get(c, "/api/now/v1/table/incident")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != 200 || r.Total == nil || *r.Total != 42 || !strings.Contains(string(r.Body), "INC1") {
		t.Errorf("response = %+v", r)
	}
	f.On("GET", "/api/now/v1/nototal", snfake.Response{Status: 200, JSON: map[string]any{"result": []any{}}})
	r, _ = get(c, "/api/now/v1/nototal")
	if r.Total != nil {
		t.Error("absent X-Total-Count must be nil")
	}
	f.On("GET", "/api/now/v1/badtotal", snfake.Response{Status: 200, JSON: map[string]any{}, Header: map[string]string{"X-Total-Count": "abc"}})
	r, _ = get(c, "/api/now/v1/badtotal")
	if r.Total != nil {
		t.Error("unparseable X-Total-Count must be nil")
	}
}

func TestRequestShape(t *testing.T) {
	f := snfake.New(t)
	f.On("POST", "/api/now/v1/table/incident", snfake.Response{Status: 201, JSON: map[string]any{}})
	c := newClient(t, f, authtest.Valid)
	q := url.Values{"sysparm_limit": {"5"}}
	if _, err := c.Do(context.Background(), sn.Call{Method: "POST", Path: "/api/now/v1/table/incident", Query: q, Body: map[string]string{"a": "b"}}); err != nil {
		t.Fatal(err)
	}
	r := f.Requests()[0]
	if r.Query != "sysparm_limit=5" || string(r.Body) != `{"a":"b"}` {
		t.Errorf("request = %+v", r)
	}
	if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" {
		t.Errorf("headers = %v", r.Header)
	}
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		t.Error("Authorization must be attached by the core authorizer")
	}
}

func TestPathMustBeAbsoluteAPIPath(t *testing.T) {
	f := snfake.New(t)
	c := newClient(t, f, authtest.Valid)
	for _, p := range []string{"", "api/now", "//evil.example.com/api", "http://evil.example.com/api", "/other/path"} {
		if _, err := get(c, p); err == nil || output.ExitOf(err) != output.ExitGeneral {
			t.Errorf("path %q must be rejected, err=%v", p, err)
		}
	}
	if len(f.Requests()) != 0 {
		t.Error("no request may be sent for a rejected path")
	}
}

func TestRefreshOnceOn401ThenSuccess(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", "/api/now/v1/x", snfake.Response{Status: 200, JSON: map[string]any{}})
	f.Fail("GET", "/api/now/v1/x", 1, snfake.Error(401, "expired"))
	c := newClient(t, f, authtest.Valid)
	if _, err := get(c, "/api/now/v1/x"); err != nil {
		t.Fatalf("single 401 must be refreshed and retried: %v", err)
	}
	if f.Count("GET", "/api/now/v1/x") != 2 {
		t.Errorf("attempts = %d", f.Count("GET", "/api/now/v1/x"))
	}
}

func TestSecond401IsExit3(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", "/api/now/v1/x", snfake.Error(401, "no"))
	c := newClient(t, f, authtest.Valid)
	_, err := get(c, "/api/now/v1/x")
	if output.ExitOf(err) != output.ExitAuth {
		t.Errorf("exit = %d (%v)", output.ExitOf(err), err)
	}
}

func TestRedirectToOtherHostIsExit4(t *testing.T) {
	other := snfake.New(t)
	other.On("GET", "/api/now/v1/x", snfake.Response{Status: 200, JSON: map[string]any{}})
	f := snfake.New(t)
	f.OnFunc("GET", "/api/now/v1/x", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL()+"/api/now/v1/x", http.StatusFound)
	})
	// "localhost" and "127.0.0.1" are different hosts for the allowlist.
	c := newClient(t, f, authtest.Valid)
	_, err := get(c, "/api/now/v1/x")
	var fh *httpx.ForbiddenHostError
	if !errors.As(err, &fh) || output.ExitOf(err) != output.ExitForbidden {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if len(other.Requests()) != 0 {
		t.Error("redirect target must never be contacted")
	}
}

func TestDaemonUnreachableIsExit3(t *testing.T) {
	f := snfake.New(t)
	c := newClient(t, f, authtest.Unreachable)
	_, err := get(c, "/api/now/v1/x")
	var ue *auth.UnreachableError
	if !errors.As(err, &ue) || output.ExitOf(err) != output.ExitAuth {
		t.Errorf("err = %v", err)
	}
	if len(f.Requests()) != 0 {
		t.Error("no request without a token")
	}
}

func TestPostRetriedOnlyWhenMarkedSafe(t *testing.T) {
	// Unmarked POST: one attempt under injected 503.
	f := snfake.New(t)
	f.On("POST", "/api/now/v1/table/incident", snfake.Response{Status: 201, JSON: map[string]any{}})
	f.Fail("POST", "/api/now/v1/table/incident", 1, snfake.Error(503, "busy"))
	c := newClient(t, f, authtest.Valid)
	_, err := c.Do(context.Background(), sn.Call{Method: "POST", Path: "/api/now/v1/table/incident", Body: map[string]string{"a": "b"}})
	if output.ExitOf(err) != output.ExitRateLimited || f.Posts() != 1 {
		t.Errorf("unmarked: exit=%d posts=%d", output.ExitOf(err), f.Posts())
	}
	// Marked POST: retried once, created once.
	g := snfake.New(t)
	g.On("POST", "/api/now/v1/table/incident", snfake.Response{Status: 201, JSON: map[string]any{}})
	g.Fail("POST", "/api/now/v1/table/incident", 1, snfake.Error(503, "busy"))
	c2 := newClient(t, g, authtest.Valid)
	r, err := c2.Do(context.Background(), sn.Call{Method: "POST", Path: "/api/now/v1/table/incident", Body: map[string]string{"a": "b"}, SafeToRetry: true})
	if err != nil || r.Status != 201 || g.Posts() != 2 {
		t.Errorf("marked: err=%v posts=%d", err, g.Posts())
	}
}

func TestNewValidatesOptions(t *testing.T) {
	au := auth.NewAuthorizer(nil)
	bad := []sn.Options{
		{Host: "", Auth: au},
		{Host: "https://acme.service-now.com", Auth: au},
		{Host: "acme.service-now.com/x", Auth: au},
		{Host: "acme.service-now.com", Insecure: true, Auth: au}, // http only for loopback
		{Host: "acme.service-now.com"},                           // auth required
	}
	for i, o := range bad {
		if _, err := sn.New(o); err == nil {
			t.Errorf("case %d: expected error for %+v", i, o)
		}
	}
	if _, err := sn.New(sn.Options{Host: "acme.service-now.com", Auth: au}); err != nil {
		t.Errorf("valid https host rejected: %v", err)
	}
	if _, err := sn.New(sn.Options{Host: "localhost:8080", Insecure: true, Auth: au}); err != nil {
		t.Errorf("loopback http rejected: %v", err)
	}
}

func TestStatusHelperOnErrors(t *testing.T) {
	var _ interface{ HTTPStatus() int } = &sn.NotFoundError{}
	e := &sn.APIError{Status: 418}
	if e.HTTPStatus() != 418 || (&sn.ValidationError{Status: 422}).HTTPStatus() != 422 || (&sn.ConflictError{Status: 409}).HTTPStatus() != 409 {
		t.Error("HTTPStatus")
	}
	if (&sn.NotFoundError{}).Hint() == "" || (&sn.ConflictError{}).Hint() == "" || (&sn.ValidationError{}).Hint() == "" || (&sn.APIError{}).Hint() == "" {
		t.Error("hints must be set")
	}
}

// ASSUMPTION A-03: ACL-hidden records appear as 404 (exit 5), not 403.
func TestAssumptionA03ACLHiddenRecordIs404ExitFive(t *testing.T) {
	f := snfake.New(t)
	// Unscripted routes answer like ServiceNow does for an ACL-hidden record.
	c := newClient(t, f, authtest.Valid)
	_, err := get(c, "/api/now/v1/table/incident/0123456789abcdef0123456789abcdef")
	var nf *sn.NotFoundError
	if !errors.As(err, &nf) || output.ExitOf(err) != output.ExitNotFound {
		t.Errorf("err = %v", err)
	}
}

// FR-R13: an unclassified 5xx is a server error, not "rate limited".
func TestServerErrorMessageIsAccurate(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", "/api/now/v1/table/incident", snfake.Error(500, "Internal failure"))
	_, err := get(newClient(t, f, authtest.Valid), "/api/now/v1/table/incident")
	var se *sn.ServerError
	if !errors.As(err, &se) || se.HTTPStatus() != 500 {
		t.Fatalf("%T %v", err, err)
	}
	msg := err.Error()
	if strings.Contains(strings.ToLower(msg), "rate limited") || !strings.Contains(msg, "HTTP 500") || !strings.Contains(msg, "server error") {
		t.Fatalf("message %q", msg)
	}
	if h := se.Hint(); strings.Contains(strings.ToLower(h), "rate") || h == "" {
		t.Fatalf("hint %q", h)
	}
}

// FR-R13: a body over the limit is reported, not truncated into a JSON error.
func TestOversizeBodyIsResponseTooLarge(t *testing.T) {
	f := snfake.New(t)
	big := append([]byte(`{"result":"`), make([]byte, 9<<20)...)
	for i := 11; i < len(big); i++ {
		big[i] = 'a'
	}
	f.On("GET", "/api/now/v1/table/incident", snfake.Response{Status: 200, Body: big})
	_, err := get(newClient(t, f, authtest.Valid), "/api/now/v1/table/incident")
	var tl *sn.ResponseTooLargeError
	if !errors.As(err, &tl) || output.ExitOf(err) != output.ExitGeneral {
		t.Fatalf("%T %v", err, err)
	}
	if !strings.Contains(err.Error(), "response too large") {
		t.Fatalf("message %q", err.Error())
	}
	// A body exactly at the limit is fine.
	ok := append([]byte(`{"result":"`), make([]byte, (8<<20)-13)...)
	for i := 11; i < len(ok); i++ {
		ok[i] = 'a'
	}
	ok = append(ok, '"', '}')
	if len(ok) != 8<<20 {
		t.Fatalf("test body is %d bytes", len(ok))
	}
	f.On("GET", "/api/now/v1/table/incident", snfake.Response{Status: 200, Body: ok})
	if _, err := get(newClient(t, f, authtest.Valid), "/api/now/v1/table/incident"); err != nil {
		t.Fatalf("a body of exactly the limit must pass: %v", err)
	}
}
