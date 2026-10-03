package idempotency

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/snow-cli/internal/domain"
)

var t0 = time.Date(2026, 10, 3, 10, 15, 0, 0, time.UTC)

func TestKeyStableWithinHourBucket(t *testing.T) {
	a := Key("agent-1", "CI-1", "Disk full", t0)
	b := Key("agent-1", "CI-1", "Disk full", t0.Add(40*time.Minute))
	if a != b {
		t.Fatalf("same hour must give same key: %s vs %s", a, b)
	}
	if c := Key("agent-1", "CI-1", "Disk full", t0.Add(46*time.Minute)); c == a {
		t.Fatal("next hour bucket must change the key")
	}
}

func TestKeyVariesByInput(t *testing.T) {
	base := Key("a", "ci", "sd", t0)
	for name, k := range map[string]string{
		"agent": Key("b", "ci", "sd", t0),
		"ci":    Key("a", "ci2", "sd", t0),
		"sd":    Key("a", "ci", "sd2", t0),
	} {
		if k == base {
			t.Errorf("%s change must change the key", name)
		}
	}
}

func TestKeyNormalisesCaseAndSpace(t *testing.T) {
	if Key("a", "ci", "  Disk Full ", t0) != Key("a", "ci", "disk full", t0) {
		t.Fatal("short description should be trimmed and case-folded")
	}
}

func TestKeyShapeAndNoSeparatorAmbiguity(t *testing.T) {
	k := Key("a", "ci", "sd", t0)
	if !strings.HasPrefix(k, "snow-") || len(k) != len("snow-")+24 {
		t.Fatalf("unexpected key shape %q", k)
	}
	if Key("a|b", "c", "d", t0) == Key("a", "b|c", "d", t0) {
		t.Fatal("field boundaries must be unambiguous")
	}
}

func TestResolvePrefersExplicit(t *testing.T) {
	if got := Resolve("my-key", "a", "ci", "sd", t0); got != "my-key" {
		t.Fatalf("got %q", got)
	}
	if got := Resolve("", "a", "ci", "sd", t0); got != Key("a", "ci", "sd", t0) {
		t.Fatalf("got %q", got)
	}
}

type finder struct {
	rec *domain.Record
	err error
	got string
}

func (f *finder) FindByCorrelation(_ context.Context, id string) (*domain.Record, error) {
	f.got = id
	return f.rec, f.err
}

func TestCheckHitMissError(t *testing.T) {
	rec := &domain.Record{Table: "incident", Fields: map[string]string{"number": "INC1"}}
	f := &finder{rec: rec}
	got, err := Check(context.Background(), f, "k")
	if err != nil || got != rec || f.got != "k" {
		t.Fatalf("hit: %v %v %q", got, err, f.got)
	}
	got, err = Check(context.Background(), &finder{}, "k")
	if err != nil || got != nil {
		t.Fatalf("miss: %v %v", got, err)
	}
	boom := errors.New("boom")
	if _, err = Check(context.Background(), &finder{err: boom}, "k"); !errors.Is(err, boom) {
		t.Fatalf("err not propagated: %v", err)
	}
}
