package sn

import (
	"context"
	"fmt"
	"net/http"

	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

// OrderAdapter implements usecase.OrderWriter.
type OrderAdapter struct{ c *Client }

var _ usecase.OrderWriter = (*OrderAdapter)(nil)

// NewOrderAdapter binds the catalog order endpoint to a client.
func NewOrderAdapter(c *Client) *OrderAdapter { return &OrderAdapter{c: c} }

// Order posts order_now. The POST is NOT marked safe to retry and
// OrderRequest.CorrelationID is not sent.
//
// ASSUMPTION(unverified against a real instance): no dedupe key is retrievable
// after order_now (A-07), so a transient failure exits 8 and the caller must
// check `snow request list` before retrying. The response shape is
// {"result":{"sys_id"|"request_id","number"|"request_number"}} (A-08).
func (a *OrderAdapter) Order(ctx context.Context, in usecase.OrderRequest) (domain.WriteResult, error) {
	if !domain.LooksLikeSysID(in.Item) {
		return domain.WriteResult{}, fmt.Errorf("sn: catalog item %q must be a sys_id", in.Item)
	}
	vars := in.Variables
	if vars == nil {
		vars = map[string]string{}
	}
	resp, err := a.c.Do(ctx, Call{
		Method: http.MethodPost, Path: "/api/sn_sc/servicecatalog/items/" + in.Item + "/order_now",
		Body: map[string]any{"sysparm_quantity": 1, "variables": vars},
	})
	if err != nil {
		return domain.WriteResult{}, err
	}
	rec, err := wDecodeOne("sc_request", resp.Body)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if n := rec.Get("request_number"); n != "" {
		rec.Fields["number"] = n
	}
	if id := rec.Get("request_id"); id != "" {
		rec.Fields["sys_id"] = id
	}
	if rec.Get("number") == "" && rec.Get("sys_id") == "" {
		return domain.WriteResult{}, fmt.Errorf("order response has neither number nor sys_id (the order response shape is unverified, A-08)")
	}
	return domain.WriteResult{Record: rec}, nil
}
