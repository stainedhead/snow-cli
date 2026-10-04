package oktafake_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stainedhead/snow-cli/internal/testsupport/oktafake"
)

func post(t *testing.T, u string, form url.Values) (*http.Response, string) {
	t.Helper()
	resp, err := http.PostForm(u, form)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	buf := new(strings.Builder)
	b := make([]byte, 4096)
	n, _ := resp.Body.Read(b)
	buf.Write(b[:n])
	return resp, buf.String()
}

func TestTokenQueueAndRecording(t *testing.T) {
	f := oktafake.New(t)
	f.QueueToken(200, `{"access_token":"a1","token_type":"Bearer","expires_in":3600}`)
	f.QueueToken(400, `{"error":"invalid_grant"}`)
	resp, body := post(t, f.Issuer()+"/v1/token", url.Values{"grant_type": {"authorization_code"}, "code": {"c"}})
	if resp.StatusCode != 200 || !strings.Contains(body, "a1") {
		t.Errorf("first = %d %s", resp.StatusCode, body)
	}
	resp, _ = post(t, f.Issuer()+"/v1/token", url.Values{"grant_type": {"refresh_token"}})
	if resp.StatusCode != 400 {
		t.Errorf("second = %d", resp.StatusCode)
	}
	reqs := f.Requests()
	if len(reqs) != 2 || reqs[0].Path != "/v1/token" || reqs[0].Form.Get("code") != "c" || reqs[1].Form.Get("grant_type") != "refresh_token" {
		t.Errorf("requests = %+v", reqs)
	}
	if f.Count("/v1/token") != 2 {
		t.Error("Count")
	}
}

func TestExhaustedQueueIsServerError(t *testing.T) {
	f := oktafake.New(t)
	resp, _ := post(t, f.Issuer()+"/v1/token", nil)
	if resp.StatusCode != 500 {
		t.Errorf("unscripted token call = %d, want 500", resp.StatusCode)
	}
}

func TestDeviceAndRevokeEndpoints(t *testing.T) {
	f := oktafake.New(t)
	f.QueueDevice(200, `{"device_code":"d","user_code":"U-1","verification_uri":"https://x/activate","interval":1,"expires_in":60}`)
	f.SetRevoke(200)
	resp, body := post(t, f.Issuer()+"/v1/device/authorize", url.Values{"client_id": {"c"}})
	if resp.StatusCode != 200 || !strings.Contains(body, "U-1") {
		t.Errorf("device = %d %s", resp.StatusCode, body)
	}
	if resp, _ := post(t, f.Issuer()+"/v1/revoke", url.Values{"token": {"t"}}); resp.StatusCode != 200 {
		t.Errorf("revoke = %d", resp.StatusCode)
	}
	f.SetRevoke(503)
	if resp, _ := post(t, f.Issuer()+"/v1/revoke", nil); resp.StatusCode != 503 {
		t.Errorf("revoke fault = %d", resp.StatusCode)
	}
	if !strings.HasPrefix(f.Host(), "127.0.0.1:") {
		t.Errorf("host = %q", f.Host())
	}
}
