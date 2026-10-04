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

func TestKeyNormalisesCIWhitespaceAndCase(t *testing.T) {
	if Key("a", "  DB01 ", "sd", t0) != Key("a", "db01", "sd", t0) {
		t.Fatal("CI names are trimmed and case-folded")
	}
	// A name and a sys_id still give different keys (documented, FR-R11).
	if Key("a", "db01", "sd", t0) == Key("a", strings.Repeat("a", 32), "sd", t0) {
		t.Fatal("name and sys_id differ")
	}
}

func TestValidateKey(t *testing.T) {
	for _, ok := range []string{"k", "my-key_1.2:3", "snow-abc", strings.Repeat("a", MaxKeyLen)} {
		if err := ValidateKey(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"has space", "a^b", "a=b", "tab\t", "new\nline", "ümlaut", "a/b", strings.Repeat("a", MaxKeyLen+1)} {
		if err := ValidateKey(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := ValidateKey(""); err != nil {
		t.Errorf("empty means 'derive': %v", err)
	}
}

func TestLookupKeysWidenWindowForDerivedKeysOnly(t *testing.T) {
	cur, prev := Key("a", "ci", "sd", t0), Key("a", "ci", "sd", t0.Add(-time.Hour))
	got := LookupKeys("", "a", "ci", "sd", t0)
	if len(got) != 2 || got[0] != cur || got[1] != prev || cur == prev {
		t.Fatalf("%v (cur %s prev %s)", got, cur, prev)
	}
	if got := LookupKeys("mine", "a", "ci", "sd", t0); len(got) != 1 || got[0] != "mine" {
		t.Fatalf("explicit key: %v", got)
	}
}

type seqFinder struct {
	byKey map[string]*domain.Record
	asked []string
	err   error
}

func (f *seqFinder) FindByCorrelation(_ context.Context, id string) (*domain.Record, error) {
	f.asked = append(f.asked, id)
	return f.byKey[id], f.err
}

func TestCheckAnyStopsAtFirstHit(t *testing.T) {
	rec := &domain.Record{Table: "incident", Fields: map[string]string{"number": "INC2"}}
	f := &seqFinder{byKey: map[string]*domain.Record{"b": rec}}
	got, err := CheckAny(context.Background(), f, []string{"a", "b", "c"})
	if err != nil || got != rec || strings.Join(f.asked, ",") != "a,b" {
		t.Fatalf("%v %v %v", got, err, f.asked)
	}
	f = &seqFinder{}
	if got, err := CheckAny(context.Background(), f, []string{"a", "b"}); got != nil || err != nil || len(f.asked) != 2 {
		t.Fatalf("miss: %v %v %v", got, err, f.asked)
	}
	boom := errors.New("boom")
	f = &seqFinder{err: boom}
	if _, err := CheckAny(context.Background(), f, []string{"a", "b"}); !errors.Is(err, boom) || len(f.asked) != 1 {
		t.Fatalf("error must stop the lookups: %v %v", err, f.asked)
	}
}
