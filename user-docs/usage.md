# Usage examples

All commands print a JSON envelope (use `--format table` or `--format text` for other renderings) and exit with a [code](troubleshooting.md#exit-codes). Add `--policy agent|human|<file>` (or set `policy.path`) and `--profile <name>` as needed; they are omitted below for brevity. Unverified: every ServiceNow-facing behaviour below was tested against fakes, not a real instance.

## Reading

### Identity

```
snow whoami
```

Calls the custom scripted identity endpoint (`whoami.path`) and returns user, display name, roles, instance, profile and mode. Your ServiceNow team must provide that endpoint (unverified).

### Tables

```
snow table get incident 0123456789abcdef0123456789abcdef --fields number,short_description
snow table list incident --query active=true --fields number,short_description --limit 25
snow table list incident --query active=true --limit 25 --offset 25
snow table count incident --query active=true
```

Options: `--query` (encoded query; `javascript:` is rejected), `--order-by field` (prefix `-` for descending; `sys_id` is always the tie-break), `--display` (display values instead of raw values). `sys_*` tables are denied by both shipped policies.

### CMDB

```
snow cmdb ci get web01
snow cmdb ci search --class cmdb_ci_server --query operational_status=1 --limit 25
snow cmdb ci related web01 --direction up --depth 2
snow cmdb app Checkout
```

`ci get` takes a sys_id (32 hex characters) or an exact name; an ambiguous name exits 9 and lists candidates. `related` walks `cmdb_rel_ci` (`down` = what the CI depends on, `up` = what depends on it); depth defaults to 2, maximum 5, and the node count is capped. `cmdb app` resolves a business service or application with owner, support group and direct related CIs.

### Work items

```
snow my work [--kind incident|request|task|change]
snow incident get INC0010001 --fields number,short_description,state
snow incident list --mine --state 2 --limit 25
snow incident list --ci web01
snow request get|list      snow ritm get|list      snow task get|list
snow change get|list       snow problem get|list
```

List filters: `--mine`, `--group`, `--state` (instance-specific value), `--query`; incidents also take `--ci` and `--app`. Work notes and comments are included when the policy allows those fields.

### Catalog

```
snow catalog search laptop --limit 10
snow catalog get <sys_id|name>
snow catalog vars <sys_id|name>
```

### Pagination and bounded output

List results are `{items, page, acl_filtered_possible}` inside `data`:

- `data.page.next_offset` is the absolute offset for the next call (null when done).
- If output was cut to fit `--max-bytes`, `meta.truncated` is true. Core's `meta.next_offset` then counts items of this page, so the next call is `--offset <previous offset + meta.next_offset>`; `data.page.next_offset` stays the better guide.
- `acl_filtered_possible: true` means a short or empty page may hide records you cannot see (ServiceNow applies the page limit before ACLs; unverified). Do not read an empty page as "nothing exists".

## Writing

Writes can be previewed with `--dry-run` (nothing is sent). In human mode each real write asks `[y/N]`; pass `--yes` to skip, which is required when there is no terminal (otherwise exit 2). Agent mode never prompts.

### Create an incident

```
snow incident create --short-description "Disk full on db01" --description "98% used" \
  --ci db01 --impact 2 --urgency 3
snow incident create --short-description "..." --description "..." --ci db01 --impact 2 --urgency 3 --dry-run
```

Required: `--short-description`, `--description`, `--ci` or `--app` (not both), `--impact`, `--urgency` (missing ones exit 9). Optional: `--assignment-group` (defaults from the CI's support group), `--note`, `--idempotency-key`. `priority` is never sent.

Repeat protection: the key defaults to a hash of agent id, CI, short description and the hour, stored in `correlation_id`. Re-running within the hour returns the existing record with `data.deduplicated: true` and sends no second create. The record also gets a provenance work note `[snow-cli agent=<id> run=<run_id>]`. Both depend on `correlation_id`/`correlation_display` existing on your instance (unverified). Agent policy: impact and urgency 1 are denied (exit 6); a human raises critical incidents.

### Update, resolve

```
snow incident update INC0010001 --work-note "restarted the service"
snow incident update INC0010001 --set state=2
snow incident resolve INC0010001 --close-code "Solved (Permanently)" --close-notes "Restarted the service"
```

Only policy-allowed fields can change. Updates re-read `sys_mod_count`; if someone else changed the record meanwhile the result is a conflict (exit 7; unverified). Resolve is denied for agents (exit 6) and allowed in the human policy.

### Update a catalog task

```
snow task update SCTASK0010001 --work-note "waiting on vendor" --comment "ETA tomorrow"
snow task update SCTASK0010001 --state 2 --assigned-to self
```

`--assigned-to` accepts only yourself, and the task must already be assigned to you.

### Order from the catalog

```
snow catalog order "Standard Laptop" --var model=x1 --dry-run
snow catalog order <sys_id> --var model=x1 --yes
```

Variables are checked against `catalog vars` first (missing mandatory or unknown variable exits 9). The agent policy allows only dry-run previews unless you add a per-item allow rule. An order is never retried automatically; after a transient failure (exit 8) check `snow request list` before ordering again.

## Selftest

```
snow selftest
snow selftest --include-writes
```

Probes the allow/deny matrix for the current identity so a ServiceNow platform team can confirm the roles are no broader than intended. It reads only by default. `--include-writes` needs `selftest.fixture_incident` and `selftest.foreign_incident` in the profile (otherwise exit 2). Exit 0 means every row behaved as expected; exit 1 lists the failing rows (for example an over-granted role). Authentication failure aborts with exit 3. It needs a live instance and is never run in the project's PR CI.

## Other

```
snow version
snow skill generate [--out file] [--check file]
```
