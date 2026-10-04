package read

import (
	"context"
	"fmt"
	"strings"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/policymap"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

// searchPageProbe is how many catalog hits are fetched when resolving an
// item by name.
const nameProbeLimit = 20

func presentItem(it domain.CatalogItem) map[string]any {
	m := map[string]any{"sys_id": it.SysID, "name": it.Name, "category": it.Category}
	if it.ShortDescription != "" {
		m["short_description"] = output.Untrusted{Value: it.ShortDescription}
	} else {
		m["short_description"] = ""
	}
	return m
}

// CatalogSearch finds catalog items by text (FR-035). The Service Catalog API
// reports no total, so the page has a next offset only for a full page.
func (s Service) CatalogSearch(ctx context.Context, text string, o ListOptions) (ListData, error) {
	if strings.TrimSpace(text) == "" {
		return ListData{}, &ValidationError{Msg: "catalog search needs search text"}
	}
	limit := s.limit(o.Limit)
	var out ListData
	err := s.guarded(ctx, policymap.VerbSearch, policymap.ResCatalogSearch, nil, func(ctx context.Context) error {
		items, err := s.Catalog.Search(ctx, text, limit, o.Offset)
		if err != nil {
			return err
		}
		out = ListData{Items: make([]map[string]any, 0, len(items))}
		for _, it := range items {
			out.Items = append(out.Items, presentItem(it))
		}
		out.Page = domain.NewPage(o.Offset, len(items), limit, nil)
		return nil
	})
	if err != nil {
		return ListData{}, err
	}
	return out, nil
}

// resolveItem turns a sys_id or an exact item name into a sys_id. A name is
// resolved by a (policy-checked) catalog search.
func (s Service) resolveItem(ctx context.Context, ref string) (string, error) {
	if domain.LooksLikeSysID(ref) {
		return strings.ToLower(ref), nil
	}
	var found []domain.CatalogItem
	err := s.guarded(ctx, policymap.VerbSearch, policymap.ResCatalogSearch, nil, func(ctx context.Context) error {
		items, err := s.Catalog.Search(ctx, ref, nameProbeLimit, 0)
		if err != nil {
			return err
		}
		for _, it := range items {
			if strings.EqualFold(it.Name, ref) {
				found = append(found, it)
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	switch len(found) {
	case 0:
		return "", &NotFoundError{Msg: fmt.Sprintf("no catalog item named %q", ref)}
	case 1:
		return found[0].SysID, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches %d catalog items; pass the sys_id of one: ", ref, len(found))
	for i, it := range found {
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s (%s) %s", it.Name, it.Category, it.SysID)
	}
	return "", &ValidationError{Msg: b.String(), Hnt: "Re-run with the sys_id of the intended item."}
}

// CatalogGet reads one catalog item by sys_id or exact name.
func (s Service) CatalogGet(ctx context.Context, ref string) (map[string]any, error) {
	id, err := s.resolveItem(ctx, ref)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	err = s.guarded(ctx, policymap.VerbGet, policymap.CatalogItem(id), nil, func(ctx context.Context) error {
		it, err := s.Catalog.Item(ctx, id)
		if err != nil {
			return err
		}
		out = presentItem(it)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// VarsData is the data of `catalog vars`.
type VarsData = usecase.CatalogVars

// CatalogVars lists the variables of a catalog item.
func (s Service) CatalogVars(ctx context.Context, ref string) (VarsData, error) {
	id, err := s.resolveItem(ctx, ref)
	if err != nil {
		return VarsData{}, err
	}
	var out VarsData
	err = s.guarded(ctx, policymap.VerbVars, policymap.CatalogItem(id), nil, func(ctx context.Context) error {
		it, err := s.Catalog.Item(ctx, id)
		if err != nil {
			return err
		}
		vars, err := s.Catalog.Variables(ctx, id)
		if err != nil {
			return err
		}
		if vars == nil {
			vars = []domain.CatalogVariable{}
		}
		out = VarsData{Item: map[string]any{"sys_id": it.SysID, "name": it.Name}, Variables: vars}
		return nil
	})
	if err != nil {
		return VarsData{}, err
	}
	return out, nil
}
