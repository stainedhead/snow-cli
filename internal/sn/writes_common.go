package sn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/domain"
)

// wFetchFields are always requested when a write reads a record first.
var wFetchFields = []string{"sys_id", "number", "sys_mod_count"}

// wSafeRef reports whether ref can be embedded in an encoded query: letters,
// digits and a few separators only, so a value cannot add query clauses.
func wSafeRef(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-', r == ':':
		default:
			return false
		}
	}
	return true
}

// wValue renders a JSON field value as text: strings as is, numbers and
// booleans formatted, reference objects by their "value".
func wValue(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		Value json.RawMessage `json:"value"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Value != nil {
		return wValue(obj.Value)
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return strconv.FormatBool(b)
	}
	return ""
}

func wFields(m map[string]json.RawMessage) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = wValue(v)
	}
	return out
}

// wDecodeOne decodes {"result":{...}} into a record.
func wDecodeOne(table string, body []byte) (domain.Record, error) {
	var r struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &r); err != nil || r.Result == nil {
		return domain.Record{}, fmt.Errorf("unexpected ServiceNow response for %s: no result object", table)
	}
	return domain.Record{Table: table, Fields: wFields(r.Result)}, nil
}

// wDecodeFirst decodes {"result":[...]} and returns the first record, if any.
func wDecodeFirst(table string, body []byte) (*domain.Record, error) {
	var r struct {
		Result []map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("unexpected ServiceNow response for %s: %w", table, err)
	}
	if len(r.Result) == 0 {
		return nil, nil
	}
	return &domain.Record{Table: table, Fields: wFields(r.Result[0])}, nil
}

// wFetch reads one record by sys_id or number.
func (c *Client) wFetch(ctx context.Context, table, ref string, extra ...string) (domain.Record, error) {
	fields := append(append([]string(nil), wFetchFields...), extra...)
	if domain.LooksLikeSysID(ref) {
		p := TableParams{Fields: fields, NoOrder: true}
		resp, err := c.Do(ctx, Call{Method: http.MethodGet, Path: TablePath(table, ref), Query: p.Values()})
		if err != nil {
			return domain.Record{}, err
		}
		return wDecodeOne(table, resp.Body)
	}
	if !wSafeRef(ref) {
		return domain.Record{}, fmt.Errorf("sn: refusing record reference %q", ref)
	}
	p := TableParams{Fields: fields, Limit: 1, Query: "number=" + ref, NoOrder: true}
	resp, err := c.Do(ctx, Call{Method: http.MethodGet, Path: TablePath(table), Query: p.Values()})
	if err != nil {
		return domain.Record{}, err
	}
	rec, err := wDecodeFirst(table, resp.Body)
	if err != nil {
		return domain.Record{}, err
	}
	if rec == nil {
		return domain.Record{}, &NotFoundError{Status: http.StatusNotFound, Message: "no " + table + " record " + ref}
	}
	return *rec, nil
}

// wModCount parses sys_mod_count; ok is false when absent or unparseable.
//
// ASSUMPTION(unverified against a real instance): sys_mod_count is readable
// and increments by one per update (A-09). When it is absent the conflict
// guard degrades to no detection rather than blocking writes.
func wModCount(r domain.Record) (int, bool) {
	n, err := strconv.Atoi(r.Get("sys_mod_count"))
	return n, err == nil
}

// wRecordName names a record in messages: its number, else its sys_id.
func wRecordName(r domain.Record, fallback string) string {
	if n := r.Get("number"); n != "" {
		return n
	}
	return fallback
}

// wGuardedPatch implements D-j: read sys_mod_count, PATCH, re-read; a jump of
// more than one means a concurrent writer (exit 7). expected > 0 additionally
// refuses to write when the current count differs from what the caller read.
//
// PATCH is deliberately never marked safe to retry: re-sending an applied
// PATCH would duplicate journal entries (work_notes, comments).
func (c *Client) wGuardedPatch(ctx context.Context, table, ref string, fields map[string]string, expected int) (domain.WriteResult, error) {
	before, err := c.wFetch(ctx, table, ref)
	if err != nil {
		return domain.WriteResult{}, err
	}
	id, ok := before.SysID()
	if !ok {
		return domain.WriteResult{}, fmt.Errorf("unexpected ServiceNow response for %s: no sys_id", table)
	}
	name := wRecordName(before, string(id))
	modBefore, haveBefore := wModCount(before)
	if expected > 0 && haveBefore && modBefore != expected {
		return domain.WriteResult{}, &ConflictError{Status: http.StatusConflict,
			Message: fmt.Sprintf("%s: sys_mod_count is %d but %d was expected: the record changed and the update was not applied", name, modBefore, expected)}
	}
	resp, err := c.Do(ctx, Call{
		Method: http.MethodPatch, Path: TablePath(table, string(id)), Body: fields,
		Query: url.Values{"sysparm_exclude_reference_link": {"true"}},
	})
	if err != nil {
		return domain.WriteResult{}, err
	}
	rec, err := wDecodeOne(table, resp.Body)
	if err != nil {
		return domain.WriteResult{}, err
	}
	// A failed re-read after a successful PATCH must not hide the write: the
	// result is returned without conflict detection.
	after, rerr := c.wFetch(ctx, table, string(id))
	if rerr != nil {
		return domain.WriteResult{Record: rec}, nil
	}
	if modAfter, haveAfter := wModCount(after); haveBefore && haveAfter && modAfter > modBefore+1 {
		return domain.WriteResult{}, &ConflictError{Status: http.StatusConflict, AppliedChange: true,
			Message: fmt.Sprintf("the change to %s was applied, but sys_mod_count advanced from %d to %d: another writer also changed the record during the update; re-read it before doing anything else", name, modBefore, modAfter)}
	}
	for k, v := range after.Fields {
		rec.Fields[k] = v
	}
	return domain.WriteResult{Record: rec}, nil
}

// ProducerConfigError reports a missing or unsafe incident.producer setting.
type ProducerConfigError struct{}

func (*ProducerConfigError) Error() string {
	return "incident.create_via is producer but incident.producer is not set to a valid record producer id"
}

// Category is validation.
func (*ProducerConfigError) Category() output.Category { return output.CategoryValidation }

// Hint tells the operator how to fix it.
func (*ProducerConfigError) Hint() string {
	return "Set incident.producer in the profile, or set incident.create_via: table."
}
