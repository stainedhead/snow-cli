package snfake_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/snow-cli/internal/testsupport/snfake"
)

func do(t *testing.T, f *snfake.Fake, method, path, body string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, f.URL()+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestFixtureResponseWithTotalCount(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", "/api/now/v1/table/incident", snfake.Records(7, map[string]any{"number": "INC1"}))
	resp, body := do(t, f, "GET", "/api/now/v1/table/incident?sysparm_limit=1", "")
	if resp.StatusCode != 200 || resp.Header.Get("X-Total-Count") != "7" {
		t.Errorf("status=%d total=%q", resp.StatusCode, resp.Header.Get("X-Total-Count"))
	}
	if !strings.Contains(body, `"number":"INC1"`) || !strings.Contains(body, `"result"`) {
		t.Errorf("body = %s", body)
	}
	reqs := f.Requests()
	if len(reqs) != 1 || reqs[0].Query != "sysparm_limit=1" || reqs[0].Path != "/api/now/v1/table/incident" {
		t.Errorf("requests = %+v", reqs)
	}
}

func TestUnmatchedIsServiceNowStyle404(t *testing.T) {
	f := snfake.New(t)
	resp, body := do(t, f, "GET", "/api/now/v1/table/nothing", "")
	if resp.StatusCode != 404 || !strings.Contains(body, `"error"`) {
		t.Errorf("status=%d body=%s", resp.StatusCode, body)
	}
}

func TestFaultInjectionStatusHeaderThenRecovers(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", "/x", snfake.Records(1))
	f.Fail("GET", "/x", 2, snfake.Response{Status: 503, Header: map[string]string{"Retry-After": "1"}})
	for i := 0; i < 2; i++ {
		resp, _ := do(t, f, "GET", "/x", "")
		if resp.StatusCode != 503 || resp.Header.Get("Retry-After") != "1" {
			t.Fatalf("call %d: status=%d hdr=%q", i, resp.StatusCode, resp.Header.Get("Retry-After"))
		}
	}
	if resp, _ := do(t, f, "GET", "/x", ""); resp.StatusCode != 200 {
		t.Errorf("after faults status = %d", resp.StatusCode)
	}
}

func TestLatencyInjection(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", "/slow", snfake.Response{Status: 200, JSON: map[string]any{}, Latency: 60 * time.Millisecond})
	start := time.Now()
	do(t, f, "GET", "/slow", "")
	if time.Since(start) < 50*time.Millisecond {
		t.Error("latency not applied")
	}
}

func TestPostCounterAndBody(t *testing.T) {
	f := snfake.New(t)
	f.On("POST", "/api/now/v1/table/incident", snfake.Response{Status: 201, JSON: map[string]any{"result": map[string]any{"sys_id": "x"}}})
	f.Fail("POST", "/api/now/v1/table/incident", 1, snfake.Response{Status: 503})
	do(t, f, "POST", "/api/now/v1/table/incident", `{"a":1}`)
	do(t, f, "POST", "/api/now/v1/table/incident", `{"a":2}`)
	do(t, f, "GET", "/api/now/v1/table/incident", "")
	if f.Posts() != 2 {
		t.Errorf("Posts = %d, want 2 (faulted requests count)", f.Posts())
	}
	if f.Count("POST", "/api/now/v1/table/incident") != 2 || f.Count("GET", "/api/now/v1/table/incident") != 1 {
		t.Error("Count")
	}
	if got := string(f.Requests()[1].Body); got != `{"a":2}` {
		t.Errorf("recorded body = %q", got)
	}
}

func TestOnFuncAndHostAndError(t *testing.T) {
	f := snfake.New(t)
	f.OnFunc("GET", "/custom", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	if resp, _ := do(t, f, "GET", "/custom", ""); resp.StatusCode != 204 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	if !strings.HasPrefix(f.Host(), "127.0.0.1:") || !strings.HasSuffix(f.URL(), f.Host()) {
		t.Errorf("host=%q url=%q", f.Host(), f.URL())
	}
	e := snfake.Error(409, "conflict")
	if e.Status != 409 {
		t.Errorf("Error() = %+v", e)
	}
	f.On("GET", "/e", e)
	if resp, body := do(t, f, "GET", "/e", ""); resp.StatusCode != 409 || !strings.Contains(body, "conflict") {
		t.Errorf("status=%d body=%s", resp.StatusCode, body)
	}
}

func TestRawBodyAndFixtureFile(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", "/raw", snfake.Response{Status: 200, Body: []byte("not json")})
	if _, body := do(t, f, "GET", "/raw", ""); body != "not json" {
		t.Errorf("body = %q", body)
	}
	r := snfake.FixtureFile(t, "testdata/sample.json")
	if r.Status != 200 || !strings.Contains(string(r.Body), "INC0000001") {
		t.Errorf("fixture = %+v", r)
	}
}
