package sn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

// Tables implements usecase.TableReader over the Table and Aggregate APIs
// (FR-006, FR-020..022).
type Tables struct{ c *Client }

var _ usecase.TableReader = (*Tables)(nil)

// NewTables binds a Client.
func NewTables(c *Client) *Tables { return &Tables{c: c} }

// Get reads one record. Single-record reads never add ordering.
func (t *Tables) Get(ctx context.Context, table, sysID string, opts usecase.GetOptions) (domain.Record, error) {
	p := TableParams{Fields: opts.Fields, Display: opts.Display, NoOrder: true}
	resp, err := t.c.Do(ctx, Call{Method: "GET", Path: TablePath(table, sysID), Query: p.Values()})
	if err != nil {
		return domain.Record{}, err
	}
	var body struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil || body.Result == nil {
		return domain.Record{}, errors.New("unexpected table response: no result object")
	}
	return recordFrom(table, body.Result), nil
}

// List reads one page. The total comes from X-Total-Count and is nil when the
// header is absent.
func (t *Tables) List(ctx context.Context, q usecase.ListQuery) (usecase.ListResult, error) {
	p := TableParams{
		Fields: q.Fields, Limit: q.Limit, Offset: q.Offset, Query: q.Query, OrderBy: q.OrderBy, Display: q.Display,
	}
	resp, err := t.c.Do(ctx, Call{Method: "GET", Path: TablePath(q.Table), Query: p.Values()})
	if err != nil {
		return usecase.ListResult{}, err
	}
	var body struct {
		Result []map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		return usecase.ListResult{}, errors.New("unexpected table response: no result array")
	}
	items := make([]domain.Record, 0, len(body.Result))
	for _, raw := range body.Result {
		items = append(items, recordFrom(q.Table, raw))
	}
	return usecase.ListResult{Items: items, Total: resp.Total}, nil
}

// Count uses the Aggregate API.
// ASSUMPTION(unverified against a real instance): A-11, GET /api/now/v1/stats/{table}?sysparm_count=true
// answering {"result":{"stats":{"count":"N"}}}.
func (t *Tables) Count(ctx context.Context, table, encodedQuery string) (int, error) {
	v := url.Values{"sysparm_count": {"true"}}
	if encodedQuery != "" {
		v.Set("sysparm_query", encodedQuery)
	}
	resp, err := t.c.Do(ctx, Call{Method: "GET", Path: StatsPath(table), Query: v})
	if err != nil {
		return 0, err
	}
	var body struct {
		Result struct {
			Stats struct {
				Count json.RawMessage `json:"count"`
			} `json:"stats"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil || len(body.Result.Stats.Count) == 0 {
		return 0, errors.New("unexpected stats response: no result.stats.count (the Aggregate API shape is unverified, A-11)")
	}
	n, err := strconv.Atoi(scalar(body.Result.Stats.Count))
	if err != nil {
		return 0, fmt.Errorf("unexpected stats count %q", scalar(body.Result.Stats.Count))
	}
	return n, nil
}

func recordFrom(table string, raw map[string]json.RawMessage) domain.Record {
	fields := make(map[string]string, len(raw))
	for k, v := range raw {
		fields[k] = scalar(v)
	}
	return domain.Record{Table: table, Fields: fields}
}

// scalar renders a JSON value as a string: strings as-is, null as "", numbers
// and booleans verbatim, display/value objects by display_value then value.
func scalar(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		for _, k := range []string{"display_value", "value"} {
			if v, ok := obj[k]; ok {
				return scalar(v)
			}
		}
	}
	if string(raw) == "null" {
		return ""
	}
	return string(raw)
}
