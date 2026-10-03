package read

import (
	"time"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/domain"
)

// untrustedFields are the free-text fields written by other people; their
// values are emitted as output.Untrusted (spec section 5, NFR-002).
var untrustedFields = map[string]bool{
	"description":             true,
	"short_description":       true,
	"work_notes":              true,
	"comments":                true,
	"comments_and_work_notes": true,
	"additional_comments":     true,
	"close_notes":             true,
	"resolution_notes":        true,
	"justification":           true,
	"work_notes_list":         false,
}

// IsUntrusted reports whether a field carries other people's free text.
func IsUntrusted(field string) bool { return untrustedFields[field] }

// snTime is how ServiceNow formats datetimes (UTC unless display values are
// requested).
const snTime = "2006-01-02 15:04:05"

// Present converts a record into response data: every field is a string,
// free-text fields are wrapped in output.Untrusted with the record's
// sys_updated_by / sys_updated_on as author and timestamp when present.
func Present(r domain.Record) map[string]any {
	out := make(map[string]any, len(r.Fields))
	author := r.Fields["sys_updated_by"]
	var ts time.Time
	if t, err := time.Parse(snTime, r.Fields["sys_updated_on"]); err == nil {
		ts = t
	}
	for k, v := range r.Fields {
		if untrustedFields[k] && v != "" {
			out[k] = output.Untrusted{Value: v, Author: author, Timestamp: ts}
			continue
		}
		out[k] = v
	}
	return out
}
