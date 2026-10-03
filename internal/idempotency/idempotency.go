// Package idempotency derives the incident idempotency key and runs the
// dedupe lookup (FR-043, spec D-c).
package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	bucket := now.UTC().Unix() / 3600
	h := sha256.New()
	for _, p := range []string{agentID, ci, strings.ToLower(strings.TrimSpace(shortDescription)), strconv.FormatInt(bucket, 10)} {
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

// Finder looks up an active record by correlation id.
type Finder interface {
	FindByCorrelation(ctx context.Context, correlationID string) (*domain.Record, error)
}

// Check runs the dedupe query. A nil record with a nil error is a miss, the
// only state in which a create may be sent (and marked safe to retry).
func Check(ctx context.Context, f Finder, key string) (*domain.Record, error) {
	return f.FindByCorrelation(ctx, key)
}
