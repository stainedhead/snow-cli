// Package policymap holds the policy verb/resource vocabulary (spec D-f), a
// stable contract shared by the use cases, the shipped policy files and the
// generated skill. Request builders for each command are added by WS-E in
// builders*.go.
package policymap

import (
	"strings"

	"github.com/stainedhead/agent-cli-core/policy"
)

// Verbs (opaque strings chosen by snow).
const (
	VerbGet      = "get"
	VerbList     = "list"
	VerbCount    = "count"
	VerbSearch   = "search"
	VerbRelated  = "related"
	VerbCreate   = "create"
	VerbUpdate   = "update"
	VerbResolve  = "resolve"
	VerbOrder    = "order"
	VerbVars     = "vars"
	VerbWhoami   = "whoami"
	VerbSelftest = "selftest"
)

// Fixed resources.
const (
	ResCMDBCI        = "cmdb:ci"
	ResCMDBApp       = "cmdb:app"
	ResIncident      = "incident"
	ResRequest       = "request"
	ResRITM          = "ritm"
	ResTask          = "task"
	ResChange        = "change"
	ResProblem       = "problem"
	ResCatalogSearch = "catalog:search"
	ResWhoami        = "whoami"
	ResSelftest      = "selftest"
)

var verbs = []string{
	VerbGet, VerbList, VerbCount, VerbSearch, VerbRelated, VerbCreate,
	VerbUpdate, VerbResolve, VerbOrder, VerbVars, VerbWhoami, VerbSelftest,
}

// Verbs returns the full verb vocabulary.
func Verbs() []string { return append([]string(nil), verbs...) }

// IsVerb reports whether v is in the vocabulary.
func IsVerb(v string) bool {
	for _, x := range verbs {
		if x == v {
			return true
		}
	}
	return false
}

// Table returns the resource for a table, "table:<name>".
func Table(name string) string { return "table:" + name }

// CatalogItem returns the resource for a catalog item, "catalog:item:<sys_id>".
func CatalogItem(sysID string) string { return "catalog:item:" + sysID }

var fixed = map[string]bool{
	ResCMDBCI: true, ResCMDBApp: true, ResIncident: true, ResRequest: true, ResRITM: true,
	ResTask: true, ResChange: true, ResProblem: true, ResCatalogSearch: true,
	ResWhoami: true, ResSelftest: true,
}

// IsResource reports whether r is a well-formed resource or resource pattern
// of the vocabulary.
func IsResource(r string) bool {
	if fixed[r] {
		return true
	}
	if rest, ok := strings.CutPrefix(r, "table:"); ok {
		return rest != ""
	}
	if rest, ok := strings.CutPrefix(r, "catalog:item:"); ok {
		return rest != ""
	}
	return false
}

// Request is a fluent wrapper over policy.Request.
type Request = policy.Request

// NewRequest starts a policy request. Builders for specific commands live in
// builders*.go (WS-E).
func NewRequest(verb, resource string) Request {
	return Request{Verb: verb, Resource: resource}
}

// WithFields returns a copy of r whose Fields carry the given field names
// (reads: the requested --fields).
func WithFields(r Request, names ...string) Request {
	f := make(map[string]any, len(names))
	for _, n := range names {
		f[n] = nil
	}
	r.Fields = f
	return r
}

// WithValues returns a copy of r whose Fields carry names and values
// (writes), so constraints on values can be evaluated.
func WithValues(r Request, values map[string]any) Request {
	f := make(map[string]any, len(values))
	for k, v := range values {
		f[k] = v
	}
	r.Fields = f
	return r
}
