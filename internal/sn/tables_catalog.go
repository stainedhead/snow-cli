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

const catalogBase = "/api/sn_sc/servicecatalog/items"

// Catalog implements usecase.CatalogReader over the Service Catalog API
// (FR-035).
//
// ASSUMPTION(unverified against a real instance): the response shapes of
// GET .../items, .../items/{id} and .../items/{id}/variables. Parsing is
// tolerant: category may be a string or an object with title/name, variable
// type may be a number or string, mandatory may be a bool or "true"/"false",
// choices may be strings or objects with value/label.
type Catalog struct{ c *Client }

var _ usecase.CatalogReader = (*Catalog)(nil)

// NewCatalog binds a Client.
func NewCatalog(c *Client) *Catalog { return &Catalog{c: c} }

// Search finds catalog items by text.
func (k *Catalog) Search(ctx context.Context, text string, limit, offset int) ([]domain.CatalogItem, error) {
	v := url.Values{"sysparm_text": {text}}
	if limit > 0 {
		v.Set("sysparm_limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		v.Set("sysparm_offset", strconv.Itoa(offset))
	}
	resp, err := k.c.Do(ctx, Call{Method: "GET", Path: catalogBase, Query: v})
	if err != nil {
		return nil, err
	}
	var body struct {
		Result []map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		return nil, errors.New("unexpected catalog response: no result array")
	}
	items := make([]domain.CatalogItem, 0, len(body.Result))
	for _, raw := range body.Result {
		items = append(items, itemFrom(raw))
	}
	return items, nil
}

// Item reads one catalog item by sys_id.
func (k *Catalog) Item(ctx context.Context, item string) (domain.CatalogItem, error) {
	if !domain.LooksLikeSysID(item) {
		return domain.CatalogItem{}, fmt.Errorf("catalog item must be a sys_id, got %q", item)
	}
	resp, err := k.c.Do(ctx, Call{Method: "GET", Path: catalogBase + "/" + item})
	if err != nil {
		return domain.CatalogItem{}, err
	}
	var body struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil || body.Result == nil {
		return domain.CatalogItem{}, errors.New("unexpected catalog response: no result object")
	}
	return itemFrom(body.Result), nil
}

// Variables lists the variables of a catalog item.
func (k *Catalog) Variables(ctx context.Context, item string) ([]domain.CatalogVariable, error) {
	if !domain.LooksLikeSysID(item) {
		return nil, fmt.Errorf("catalog item must be a sys_id, got %q", item)
	}
	resp, err := k.c.Do(ctx, Call{Method: "GET", Path: catalogBase + "/" + item + "/variables"})
	if err != nil {
		return nil, err
	}
	var body struct {
		Result []map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		return nil, errors.New("unexpected catalog response: no result array")
	}
	vars := make([]domain.CatalogVariable, 0, len(body.Result))
	for _, raw := range body.Result {
		vars = append(vars, domain.CatalogVariable{
			Name: scalar(raw["name"]), Label: scalar(raw["label"]), Type: scalar(raw["type"]),
			Mandatory: scalar(raw["mandatory"]) == "true", Choices: choicesFrom(raw["choices"]),
		})
	}
	return vars, nil
}

func itemFrom(raw map[string]json.RawMessage) domain.CatalogItem {
	return domain.CatalogItem{
		SysID: scalar(raw["sys_id"]), Name: scalar(raw["name"]),
		ShortDescription: scalar(raw["short_description"]), Category: categoryText(raw["category"]),
	}
}

func categoryText(raw json.RawMessage) string {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) == nil {
		for _, k := range []string{"title", "name", "label"} {
			if v, ok := obj[k]; ok {
				return scalar(v)
			}
		}
		return ""
	}
	return scalar(raw)
}

func choicesFrom(raw json.RawMessage) []string {
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, c := range list {
		var obj map[string]json.RawMessage
		if json.Unmarshal(c, &obj) == nil {
			if v, ok := obj["value"]; ok {
				out = append(out, scalar(v))
				continue
			}
			out = append(out, scalar(obj["label"]))
			continue
		}
		out = append(out, scalar(c))
	}
	return out
}
