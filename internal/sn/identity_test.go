package sn_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/sn"
	"github.com/stainedhead/snow-cli/internal/testsupport/snfake"
)

const whoamiPath = "/api/x_corp_agent/v1/whoami"

// ASSUMPTION(unverified against a real instance): the scripted whoami
// endpoint and response shape (A-02).
func TestAssumptionWhoamiResponseShape(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", whoamiPath, snfake.Response{Status: 200, JSON: map[string]any{"result": map[string]any{
		"user_name": "svc.agent", "name": "Service Agent", "roles": []string{"itil", "cmdb_read"},
	}}})
	id, err := sn.NewIdentity(newClient(t, f, authtest.Valid), whoamiPath).Whoami(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if id.User != "svc.agent" || id.DisplayName != "Service Agent" || strings.Join(id.Roles, ",") != "itil,cmdb_read" {
		t.Errorf("identity = %+v", id)
	}
}

func TestAssumptionWhoamiRolesAsCommaString(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", whoamiPath, snfake.Response{Status: 200, JSON: map[string]any{"result": map[string]any{
		"user_name": "u", "roles": "itil, cmdb_read",
	}}})
	id, err := sn.NewIdentity(newClient(t, f, authtest.Valid), whoamiPath).Whoami(context.Background())
	if err != nil || strings.Join(id.Roles, ",") != "itil,cmdb_read" {
		t.Errorf("identity = %+v err=%v", id, err)
	}
}

func TestWhoamiUnexpectedBody(t *testing.T) {
	f := snfake.New(t)
	c := newClient(t, f, authtest.Valid)
	for name, body := range map[string][]byte{
		"not json": []byte("<html>"), "no user": []byte(`{"result":{}}`), "empty": []byte(`{}`),
	} {
		f.On("GET", whoamiPath, snfake.Response{Status: 200, Body: body})
		_, err := sn.NewIdentity(c, whoamiPath).Whoami(context.Background())
		if err == nil || output.ExitOf(err) != output.ExitGeneral {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestWhoamiEndpointErrorsUseStatusMap(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", whoamiPath, snfake.Error(404, "no such endpoint"))
	_, err := sn.NewIdentity(newClient(t, f, authtest.Valid), whoamiPath).Whoami(context.Background())
	if output.ExitOf(err) != output.ExitNotFound {
		t.Errorf("exit = %d", output.ExitOf(err))
	}
}
