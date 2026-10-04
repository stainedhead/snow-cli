package read

import (
	"regexp"
	"time"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/domain"
)

// shape names the only value form a structured field may take to stay a plain
// string (FR-R12). The check is per field so that, for example, a reference
// returned as a display name or a hyphenated phrase in a "code" field is still
// marked untrusted.
type shape int

const (
	shapeID     shape = iota + 1 // sys_id, also references returned as sys_ids
	shapeNumber                  // INC0010001
	shapeClass                   // table / class name
	shapeCode                    // short lower-case choice value or number: 2, normal
	shapeBool                    // true / false
	shapeInt                     // digits
	shapeTime                    // 2026-10-01 10:00:00
	shapeUser                    // user_name: lower-case login id
	shapeKey                     // correlation id: letters, digits . _ - :
)

var shapePatterns = map[shape]*regexp.Regexp{
	shapeID:     regexp.MustCompile(`^[0-9a-fA-F]{32}$`),
	shapeNumber: regexp.MustCompile(`^[A-Za-z]{2,10}[0-9]{1,12}$`),
	shapeClass:  regexp.MustCompile(`^[a-z][a-z0-9_]{0,79}$`),
	shapeCode:   regexp.MustCompile(`^[a-z0-9_.]{1,40}$`),
	shapeBool:   regexp.MustCompile(`^(true|false)$`),
	shapeInt:    regexp.MustCompile(`^[0-9]{1,12}$`),
	shapeTime:   regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}( [0-9]{2}:[0-9]{2}:[0-9]{2})?$`),
	shapeUser:   regexp.MustCompile(`^[a-z0-9_.@]{1,60}$`),
	shapeKey:    regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`),
}

// structuredFields is the documented set of fields whose values are
// identifiers, codes, flags, numbers, timestamps or references rather than
// free text. Every other field is treated as written by someone else and is
// emitted as output.Untrusted. A listed field stays plain only when its value
// has the listed shape.
var structuredFields = func() map[string]shape {
	m := map[string]shape{
		"sys_id": shapeID, "number": shapeNumber, "sys_class_name": shapeClass, "sys_mod_count": shapeInt,
		"active": shapeBool, "correlation_id": shapeKey,
		"sys_created_by": shapeUser, "sys_updated_by": shapeUser,
	}
	for _, f := range []string{"sys_created_on", "sys_updated_on", "opened_at", "closed_at", "resolved_at", "due_date", "start_date", "end_date"} {
		m[f] = shapeTime
	}
	for _, f := range []string{"state", "stage", "request_state", "approval", "priority", "impact", "urgency", "severity",
		"type", "risk", "operational_status", "install_status", "environment", "business_criticality"} {
		m[f] = shapeCode
	}
	// References: plain only when returned as sys_ids (a display name is text).
	for _, f := range []string{"assigned_to", "assignment_group", "caller_id", "opened_by", "requested_for", "cmdb_ci",
		"business_service", "support_group", "owned_by", "managed_by", "parent", "child", "cat_item", "request", "request_item"} {
		m[f] = shapeID
	}
	return m
}()

// IsUntrusted reports whether a field is, by default, free text written by
// someone else: every field outside the documented structured set.
func IsUntrusted(field string) bool { _, ok := structuredFields[field]; return !ok }

// structured reports whether v is a plain-string-safe value of field.
func structured(field, v string) bool {
	sh, ok := structuredFields[field]
	return ok && shapePatterns[sh].MatchString(v)
}

// snTime is how ServiceNow formats datetimes (UTC unless display values are
// requested).
const snTime = "2006-01-02 15:04:05"

// mark returns v as a plain string when field is structured and v looks
// structured (or v is empty), else as output.Untrusted with the given author
// and timestamp.
func mark(field, v, author string, ts time.Time) any {
	if v == "" || structured(field, v) {
		return v
	}
	return output.Untrusted{Value: v, Author: author, Timestamp: ts}
}

// markText marks v as untrusted whenever it is non-empty, for fields that are
// free text by nature (names, labels, choices).
func markText(v string) any {
	if v == "" {
		return v
	}
	return output.Untrusted{Value: v}
}

// Present converts a record into response data: every field is a string,
// every field outside the documented structured set is wrapped in
// output.Untrusted with the record's sys_updated_by / sys_updated_on as author
// and timestamp when present (FR-R12).
func Present(r domain.Record) map[string]any {
	out := make(map[string]any, len(r.Fields))
	author := r.Fields["sys_updated_by"]
	var ts time.Time
	if t, err := time.Parse(snTime, r.Fields["sys_updated_on"]); err == nil {
		ts = t
	}
	for k, v := range r.Fields {
		out[k] = mark(k, v, author, ts)
	}
	return out
}
