package sn

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

// Identity implements usecase.Identity over the scripted whoami endpoint.
//
// ASSUMPTION(unverified against a real instance): the custom scripted whoami
// endpoint path (configurable) and the response shape
// {"result":{"user_name","name","roles"}} with roles as an array or a
// comma-separated string (A-02).
type Identity struct {
	c    *Client
	path string
}

var _ usecase.Identity = (*Identity)(nil)

// NewIdentity binds the whoami endpoint path to a client.
func NewIdentity(c *Client, path string) *Identity { return &Identity{c: c, path: path} }

// Whoami fetches the caller's identity.
func (i *Identity) Whoami(ctx context.Context) (domain.Identity, error) {
	resp, err := i.c.Do(ctx, Call{Method: "GET", Path: i.path})
	if err != nil {
		return domain.Identity{}, err
	}
	var body struct {
		Result struct {
			UserName string          `json:"user_name"`
			Name     string          `json:"name"`
			Roles    json.RawMessage `json:"roles"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil || body.Result.UserName == "" {
		return domain.Identity{}, errors.New("unexpected whoami response: no result.user_name (the scripted endpoint shape is unverified, A-02)")
	}
	return domain.Identity{
		User: body.Result.UserName, DisplayName: body.Result.Name, Roles: parseRoles(body.Result.Roles),
	}, nil
}

func parseRoles(raw json.RawMessage) []string {
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return nil
	}
	var out []string
	for _, r := range strings.Split(s, ",") {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}
