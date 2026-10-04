package auditx

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/policy"
)

// Cross-process rate limits (FR-R02, decision S0.1 option a).
//
// The core policy engine counts in memory, and every `snow` call is its own
// process, so its per_hour and per_run limits never trigger. StateLimiter
// enforces the same limits from a small JSON state file guarded by flock:
//
//   - per_hour: a sliding hour window of admitted-request times, keyed by
//     agent id and limit (the rule id, or the policy-wide limit).
//   - per_run: a counter keyed by agent id, run id and limit. A run is one
//     SNOW_RUN_ID; without it every invocation gets a fresh run id, so
//     per_run then only bounds a single process.
//
// Only requests the policy would let run consume budget, like the core.

// RateLimiter admits or refuses a policy-allowed decision against the
// cross-process limits of p. A refusal is a deny Decision with RetryAfter
// when the wait is known; an error means the limit state is unavailable.
type RateLimiter interface {
	Admit(p *policy.Policy, d policy.Decision) (policy.Decision, error)
}

const (
	globalLimitKey   = "policy"
	runRetention     = 7 * 24 * time.Hour
	stateFileVersion = 1
)

// StateLimiter implements RateLimiter on a locked state file.
type StateLimiter struct {
	// Path is the state file; its directory is created (0700) on demand.
	Path    string
	AgentID string
	RunID   string
	// Now supplies time; nil uses time.Now.
	Now func() time.Time
}

var _ RateLimiter = (*StateLimiter)(nil)

type runEntry struct {
	Count   int   `json:"count"`
	Updated int64 `json:"updated"` // unix nanoseconds
}

type limitState struct {
	Version int                 `json:"version"`
	Hits    map[string][]int64  `json:"hits"` // agent|limit -> unix nanoseconds
	Runs    map[string]runEntry `json:"runs"` // agent|run|limit
}

// RateStateError reports that the cross-process limit state could not be
// read or written; requests under a limited rule are refused (exit 1).
type RateStateError struct {
	Path string
	Err  error
}

func (e *RateStateError) Error() string {
	return fmt.Sprintf("rate-limit state %s is unavailable; the request was not sent: %v", e.Path, e.Err)
}

// Unwrap returns the cause.
func (e *RateStateError) Unwrap() error { return e.Err }

// Category is general (exit 1).
func (*RateStateError) Category() output.Category { return output.CategoryGeneral }

// Hint advises on the state file.
func (e *RateStateError) Hint() string {
	return "Fix permissions or free space for " + e.Path + ", or remove a corrupt file; limited actions are refused while the state is unreadable."
}

type limitDef struct {
	key, who string
	rate     policy.Rate
}

// limitsFor lists the limits that apply to the rule that decided d.
func limitsFor(p *policy.Policy, d policy.Decision) []limitDef {
	var out []limitDef
	if p == nil {
		return nil
	}
	if r := p.RateLimit; r.PerHour > 0 || r.PerRun > 0 {
		out = append(out, limitDef{globalLimitKey, "policy", r})
	}
	for i := range p.Rules {
		if p.Rules[i].ID != d.RuleID {
			continue
		}
		if r := p.Rules[i].RateLimit; r.PerHour > 0 || r.PerRun > 0 {
			out = append(out, limitDef{"rule:" + d.RuleID, "rule " + d.RuleID, r})
		}
	}
	return out
}

// Admit implements RateLimiter.
func (l *StateLimiter) Admit(p *policy.Policy, d policy.Decision) (policy.Decision, error) {
	if !d.Allowed {
		return d, nil
	}
	limits := limitsFor(p, d)
	if len(limits) == 0 {
		return d, nil
	}
	now := time.Now
	if l.Now != nil {
		now = l.Now
	}
	t := now()
	var out policy.Decision
	err := withLock(l.Path, func(f *os.File) error {
		st, err := readState(f)
		if err != nil {
			return err
		}
		out = l.apply(st, limits, d, t)
		if !out.Allowed {
			return nil // a refusal consumes no budget, nothing to save
		}
		return writeState(f, st)
	})
	if err != nil {
		return policy.Decision{}, &RateStateError{Path: l.Path, Err: err}
	}
	return out, nil
}

func (l *StateLimiter) apply(st *limitState, limits []limitDef, d policy.Decision, now time.Time) policy.Decision {
	st.prune(now)
	cut := now.Add(-time.Hour).UnixNano()
	for _, lim := range limits {
		hk := l.AgentID + "|" + lim.key
		rk := l.AgentID + "|" + l.RunID + "|" + lim.key
		if lim.rate.PerRun > 0 && st.Runs[rk].Count >= lim.rate.PerRun {
			return refused(d.RuleID, fmt.Sprintf("%s per-run rate limit reached (%d for run %q, counted across invocations)", lim.who, lim.rate.PerRun, l.RunID), 0)
		}
		if lim.rate.PerHour > 0 {
			w := trimHits(st.Hits[hk], cut)
			st.Hits[hk] = w
			if len(w) >= lim.rate.PerHour {
				wait := time.Unix(0, w[0]).Add(time.Hour).Sub(now)
				return refused(d.RuleID, fmt.Sprintf("%s hourly rate limit reached (%d per hour, counted across invocations); retry in %s", lim.who, lim.rate.PerHour, wait.Round(time.Second)), wait)
			}
		}
	}
	for _, lim := range limits {
		if lim.rate.PerRun > 0 {
			rk := l.AgentID + "|" + l.RunID + "|" + lim.key
			e := st.Runs[rk]
			e.Count++
			e.Updated = now.UnixNano()
			st.Runs[rk] = e
		}
		if lim.rate.PerHour > 0 {
			hk := l.AgentID + "|" + lim.key
			st.Hits[hk] = append(st.Hits[hk], now.UnixNano())
		}
	}
	return d
}

func refused(rule, reason string, retry time.Duration) policy.Decision {
	return policy.Decision{Mode: policy.ModeDeny, RuleID: rule, Reason: reason, RetryAfter: retry}
}

func trimHits(w []int64, cut int64) []int64 {
	i := 0
	for i < len(w) && w[i] <= cut {
		i++
	}
	return w[i:]
}

// prune drops hour windows that emptied and run counters idle for a week, so
// the file stays small.
func (s *limitState) prune(now time.Time) {
	cut := now.Add(-time.Hour).UnixNano()
	for k, w := range s.Hits {
		if w = trimHits(w, cut); len(w) == 0 {
			delete(s.Hits, k)
		} else {
			s.Hits[k] = w
		}
	}
	old := now.Add(-runRetention).UnixNano()
	for k, e := range s.Runs {
		if e.Updated < old {
			delete(s.Runs, k)
		}
	}
}

func readState(f *os.File) (*limitState, error) {
	st := &limitState{Version: stateFileVersion, Hits: map[string][]int64{}, Runs: map[string]runEntry{}}
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if fi.Size() == 0 {
		return st, nil
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	if err := json.NewDecoder(f).Decode(st); err != nil {
		return nil, fmt.Errorf("corrupt state: %w", err)
	}
	if st.Version != stateFileVersion {
		return nil, errors.New("unsupported state version")
	}
	if st.Hits == nil {
		st.Hits = map[string][]int64{}
	}
	if st.Runs == nil {
		st.Runs = map[string]runEntry{}
	}
	return st, nil
}

func writeState(f *os.File, st *limitState) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.WriteAt(b, 0); err != nil {
		return err
	}
	return f.Sync()
}

// withLock opens (creating, 0600) the state file under an exclusive flock and
// runs fn.
func withLock(path string, fn func(*os.File) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := lockFile(f); err != nil {
		return err
	}
	defer unlockFile(f)
	return fn(f)
}
