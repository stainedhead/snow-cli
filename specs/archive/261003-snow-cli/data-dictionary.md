# Data Dictionary: snow-cli (2026-10-03)

Purpose: names and shapes of data structures; refine during implementation.

## Entities
- Record: generic table record map[string]string plus Table name and sys_id.
- Incident, Request, RequestItem, CatalogTask, Change, Problem, CI, CatalogItem, CatalogVariable (typed projections of Record).
## Value objects
- SysID (32 hex), Number (INC/REQ/RITM/SCTASK/CHG/PRB prefix + digits), EncodedQuery, ImpactUrgency (1,2,3 with scale config), CorrelationKey, Page{Offset,Returned,Total,NextOffset}, Identity{User,Mode,Profile,AgentID,RunID}.
## Interfaces (ports)
- TableReader, CatalogClient, IncidentWriter, TaskWriter, Whoami, TokenSource (core auth), Keychain (Store), OktaClient, Clock, Prompter, BrowserLauncher, AuditSink (core audit.Logger), PolicyEngine (core).
## Enumerations
- Mode: agent|human. Verb and Resource vocabulary (spec D-f). Exit codes/categories from core output.
## API request/response types
- ListResponse{items,page,acl_filtered_possible}; WriteResult{record,deduplicated,dry_run}; WhoamiResponse [TBD per A-02]; SN error body {error:{message,detail},status}.
