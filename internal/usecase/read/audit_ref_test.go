package read_test

import (
	"context"
	"testing"

	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase/read"
)

// FR-R10: single-record reads carry the target reference for the audit
// record; policy keeps matching on the base resource.
func TestSingleRecordReadsCarryRefs(t *testing.T) {
	ctx := context.Background()
	ft := &fakeTables{records: map[string][]domain.Record{
		"incident":        {rec("incident", "sys_id", sid1, "number", "INC0010001")},
		"cmdb_ci":         {rec("cmdb_ci", "sys_id", sid2, "name", "web 01", "sys_class_name", "cmdb_ci_server")},
		"cmdb_ci_service": {rec("cmdb_ci_service", "sys_id", sid3, "name", "payments")},
		"cmdb_rel_ci":     nil,
	}}
	g := newGuard(t, allowAll)
	s := svc(t, ft, g)

	if _, err := s.TableGet(ctx, "incident", sid1, read.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WorkGet(ctx, read.KindIncident, "inc0010001", read.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CIGet(ctx, "web 01", read.Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CIRelated(ctx, sid2, "down", 1); err != nil {
		t.Fatal(err)
	}
	want := []string{sid1, "INC0010001", "web_01", sid2}
	if len(g.Refs) != len(want) {
		t.Fatalf("refs %v", g.Refs)
	}
	for i := range want {
		if g.Refs[i] != want[i] {
			t.Errorf("ref %d = %q, want %q", i, g.Refs[i], want[i])
		}
	}
	if g.Requests[0].Resource != "table:incident" || g.Requests[1].Resource != "incident" {
		t.Errorf("policy resources %v", g.Requests)
	}
}

func TestListReadsHaveNoRef(t *testing.T) {
	g := newGuard(t, allowAll)
	if _, err := svc(t, incidentTables(), g).TableList(context.Background(), "incident", read.ListOptions{}); err != nil {
		t.Fatal(err)
	}
	if g.Refs[0] != "" {
		t.Fatalf("ref %q", g.Refs[0])
	}
}
