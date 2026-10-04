package policymap

// Request builders (spec D-f): one function per command shape so use cases
// never spell verb/resource strings themselves. Read builders carry the
// requested field names; write builders carry field values so constraints
// (for example the impact/urgency scale, D-d) can be evaluated. Priority is
// never filtered out here: if a caller offers it the policy denies it.

// TableGet is `table get`, and the policy request for any single-table read.
func TableGet(table string, fields ...string) Request {
	return WithFields(NewRequest(VerbGet, Table(table)), fields...)
}

// TableList is `table list`.
func TableList(table string, fields ...string) Request {
	return WithFields(NewRequest(VerbList, Table(table)), fields...)
}

// TableCount is `table count`.
func TableCount(table string) Request {
	return NewRequest(VerbCount, Table(table))
}

// CIGet is `cmdb ci get`.
func CIGet(fields ...string) Request {
	return WithFields(NewRequest(VerbGet, ResCMDBCI), fields...)
}

// CISearch is `cmdb ci search`.
func CISearch(fields ...string) Request {
	return WithFields(NewRequest(VerbSearch, ResCMDBCI), fields...)
}

// CIRelated is `cmdb ci related`.
func CIRelated(fields ...string) Request {
	return WithFields(NewRequest(VerbRelated, ResCMDBCI), fields...)
}

// AppGet is `cmdb app`.
func AppGet(fields ...string) Request {
	return WithFields(NewRequest(VerbGet, ResCMDBApp), fields...)
}

// WorkGet is a typed work-item get; resource is one of ResIncident,
// ResRequest, ResRITM, ResTask, ResChange, ResProblem.
func WorkGet(resource string, fields ...string) Request {
	return WithFields(NewRequest(VerbGet, resource), fields...)
}

// WorkList is a typed work-item list (also `my work` with ResTask).
func WorkList(resource string, fields ...string) Request {
	return WithFields(NewRequest(VerbList, resource), fields...)
}

// CatalogSearch is `catalog search`.
func CatalogSearch() Request { return NewRequest(VerbSearch, ResCatalogSearch) }

// CatalogGet is `catalog get <item>`.
func CatalogGet(itemSysID string) Request {
	return NewRequest(VerbGet, CatalogItem(itemSysID))
}

// CatalogVars is `catalog vars <item>`.
func CatalogVars(itemSysID string) Request {
	return NewRequest(VerbVars, CatalogItem(itemSysID))
}

// CatalogOrder is `catalog order <item>`; vars are the order variables.
func CatalogOrder(itemSysID string, vars map[string]any) Request {
	return WithValues(NewRequest(VerbOrder, CatalogItem(itemSysID)), vars)
}

// IncidentCreate is `incident create`; values are the payload fields.
func IncidentCreate(values map[string]any) Request {
	return WithValues(NewRequest(VerbCreate, ResIncident), values)
}

// IncidentUpdate is `incident update`.
func IncidentUpdate(values map[string]any) Request {
	return WithValues(NewRequest(VerbUpdate, ResIncident), values)
}

// IncidentResolve is `incident resolve`.
func IncidentResolve(values map[string]any) Request {
	return WithValues(NewRequest(VerbResolve, ResIncident), values)
}

// TaskUpdate is `task update` (sc_task).
func TaskUpdate(values map[string]any) Request {
	return WithValues(NewRequest(VerbUpdate, ResTask), values)
}

// Whoami is `whoami`.
func Whoami() Request { return NewRequest(VerbWhoami, ResWhoami) }

// Selftest is `selftest`.
func Selftest() Request { return NewRequest(VerbSelftest, ResSelftest) }
