package read

import (
	"context"

	"github.com/stainedhead/snow-cli/internal/policymap"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

// genericFields is the field set sent for generic table reads when neither
// --fields nor a policy allowlist is available: sysparm_fields is never empty.
var genericFields = []string{"sys_id", "number", "name", "short_description", "sys_updated_on"}

// CountData is the data of `table count`.
type CountData struct {
	Table string `json:"table"`
	Count int    `json:"count"`
}

// TableGet reads one record from a table (FR-020).
func (s Service) TableGet(ctx context.Context, table, sysID string, o Options) (map[string]any, error) {
	if err := validTable(table); err != nil {
		return nil, err
	}
	if err := validSysID(sysID); err != nil {
		return nil, err
	}
	res := policymap.Table(table)
	fields := s.effectiveFields(policymap.VerbGet, res, o.Fields, genericFields)
	var out map[string]any
	err := s.guarded(ctx, policymap.VerbGet, res, fields, func(ctx context.Context) error {
		r, err := s.Tables.Get(ctx, table, sysID, usecase.GetOptions{Fields: fields, Display: o.Display})
		if err != nil {
			return err
		}
		out = Present(r)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// TableList reads one page from a table (FR-021, D-h).
func (s Service) TableList(ctx context.Context, table string, o ListOptions) (ListData, error) {
	res := policymap.Table(table)
	fields := s.effectiveFields(policymap.VerbList, res, o.Fields, genericFields)
	return s.list(ctx, policymap.VerbList, res, table, noQuery, fields, o)
}

// TableCount counts matching records through the Aggregate API (FR-022).
func (s Service) TableCount(ctx context.Context, table, query string) (CountData, error) {
	if err := validTable(table); err != nil {
		return CountData{}, err
	}
	if err := ValidateQuery(query); err != nil {
		return CountData{}, err
	}
	var out CountData
	err := s.guarded(ctx, policymap.VerbCount, policymap.Table(table), nil, func(ctx context.Context) error {
		n, err := s.Tables.Count(ctx, table, query)
		if err != nil {
			return err
		}
		out = CountData{Table: table, Count: n}
		return nil
	})
	if err != nil {
		return CountData{}, err
	}
	return out, nil
}
