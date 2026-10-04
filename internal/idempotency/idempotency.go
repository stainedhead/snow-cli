// Package idempotency derives the incident idempotency key and runs the
// dedupe lookup (FR-043, spec D-c).
package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/stainedhead/snow-cli/internal/domain"
)

const keyPrefix = "snow-"

// keyHexLen is the number of hex characters kept from the digest.
const keyHexLen = 24

// Key is sha256(agent_id|ci|short_description|hour_bucket) truncated. Each
// component is length-prefixed so field boundaries cannot be confused. The
// short description is trimmed and case-folded so cosmetic retries match.
func Key(agentID, ci, shortDescription string, now time.Time) string {
	return keyAt(agentID, ci, shortDescription, now.UTC().Unix()/3600)
}

// keyAt derives the key for one hour bucket. The CI and short description are
// trimmed and case-folded; a CI name and a sys_id for the same CI still give
// different keys, because resolving a name costs a request (FR-R11).
func keyAt(agentID, ci, shortDescription string, bucket int64) string {
	h := sha256.New()
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	for _, p := range []string{agentID, norm(ci), norm(shortDescription), strconv.FormatInt(bucket, 10)} {
		h.Write([]byte(strconv.Itoa(len(p)) + ":" + p + "|"))
	}
	return keyPrefix + hex.EncodeToString(h.Sum(nil))[:keyHexLen]
}

// Resolve returns the explicit --idempotency-key when given, else the
// derived default.
func Resolve(explicit, agentID, ci, shortDescription string, now time.Time) string {
	if explicit != "" {
		return explicit
	}
	return Key(agentID, ci, shortDescription, now)
}

// MaxKeyLen bounds an explicit --idempotency-key.
const MaxKeyLen = 64

// ValidateKey checks an explicit key's character set (letters, digits and
// . _ - :) and length. An empty key is valid: it selects the derived key.
// Callers run it before the guard, so a bad key exits 9 with no audit record
// and no request (FR-R11).
func ValidateKey(k string) error {
	if k == "" {
		return nil
	}
	if len(k) > MaxKeyLen {
		return fmt.Errorf("--idempotency-key is longer than %d characters", MaxKeyLen)
	}
	for _, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-', r == ':':
		default:
			return fmt.Errorf("--idempotency-key %q may contain only letters, digits and the characters . _ - and colon", k)
		}
	}
	return nil
}

// LookupKeys are the correlation ids the dedupe query checks, newest first.
// A derived key also checks the previous hour bucket, so a retry across the
// hour boundary still finds the record; an explicit key is looked up as is.
// The create always uses Resolve's key.
func LookupKeys(explicit, agentID, ci, shortDescription string, now time.Time) []string {
	if explicit != "" {
		return []string{explicit}
	}
	bucket := now.UTC().Unix() / 3600
	return []string{keyAt(agentID, ci, shortDescription, bucket), keyAt(agentID, ci, shortDescription, bucket-1)}
}

// Finder looks up a record by correlation id.
type Finder interface {
	FindByCorrelation(ctx context.Context, correlationID string) (*domain.Record, error)
}

// Check runs the dedupe query. A nil record with a nil error is a miss, the
// only state in which a create may be sent (and marked safe to retry).
func Check(ctx context.Context, f Finder, key string) (*domain.Record, error) {
	return f.FindByCorrelation(ctx, key)
}

// CheckAny runs the dedupe query for each key in order and returns the first
// hit. A lookup error stops the search: a create must not be sent when the
// state is unknown.
func CheckAny(ctx context.Context, f Finder, keys []string) (*domain.Record, error) {
	for _, k := range keys {
		hit, err := f.FindByCorrelation(ctx, k)
		if err != nil || hit != nil {
			return hit, err
		}
	}
	return nil, nil
}
