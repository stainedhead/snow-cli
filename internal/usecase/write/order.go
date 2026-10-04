package write

import (
	"context"
	"strings"

	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/policymap"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

// OrderService orders catalog items (FR-046).
type OrderService struct {
	Base
	// Catalog is the guarded read service: item resolution and variable
	// definitions go through policy and audit (FR-R03).
	Catalog usecase.GuardedCatalog
	Writer  usecase.OrderWriter
	// IdempotencyKey is passed through as the order correlation id. Order
	// POSTs are never retried and never deduplicated until A-07 is confirmed.
	IdempotencyKey string
}

// Order validates the variables against the item's definition (exit 9 on a
// missing mandatory or unknown variable), then checks policy against the
// resolved item. The default policy for orders is dry_run_only.
func (s OrderService) Order(ctx context.Context, item string, vars map[string]string) (domain.WriteResult, error) {
	if blank(item) {
		return domain.WriteResult{}, invalid("a catalog item name or sys_id is required")
	}
	vd, err := s.Catalog.CatalogVars(ctx, item)
	if err != nil {
		return domain.WriteResult{}, err
	}
	it := domain.CatalogItem{}
	it.SysID, _ = vd.Item["sys_id"].(string)
	it.Name, _ = vd.Item["name"].(string)
	defs := vd.Variables
	if err := validateVariables(defs, vars); err != nil {
		return domain.WriteResult{}, err
	}
	req := policymap.WithValues(policymap.NewRequest(policymap.VerbOrder, policymap.CatalogItem(it.SysID)), policyValues(vars))
	var out domain.WriteResult
	err = s.Guard.Run(ctx, usecase.Action{Kind: usecase.Write, Request: req}, func(ctx context.Context, d policy.Decision) (int, error) {
		if s.preview(ctx, d) {
			f := map[string]string{"item": it.SysID, "name": it.Name}
			for k, v := range vars {
				f["var."+k] = v
			}
			out = previewResult("sc_request", f)
			return 0, nil
		}
		if err := s.confirm("Order catalog item \"" + it.Name + "\" (" + it.SysID + ")?"); err != nil {
			return 0, err
		}
		res, err := s.Writer.Order(ctx, usecase.OrderRequest{Item: it.SysID, Variables: vars, CorrelationID: s.IdempotencyKey})
		if err != nil {
			return 0, err
		}
		out = res
		return statusOK, nil
	})
	if err != nil {
		return domain.WriteResult{}, err
	}
	return out, nil
}

func validateVariables(defs []domain.CatalogVariable, vars map[string]string) error {
	known := make(map[string]bool, len(defs))
	var missing []string
	for _, d := range defs {
		known[d.Name] = true
		if d.Mandatory && vars[d.Name] == "" {
			missing = append(missing, d.Name)
		}
	}
	if len(missing) > 0 {
		return invalid("missing mandatory variable(s): %s", strings.Join(missing, ", "))
	}
	for _, k := range sortedKeys(vars) {
		if !known[k] {
			return invalid("unknown variable %q for this item (see `snow catalog vars`)", k)
		}
	}
	return nil
}
