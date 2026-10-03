package read_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase/read"
)

func loadFixtureRecords(t *testing.T) []domain.Record {
	t.Helper()
	b, err := os.ReadFile("../../../testdata/fixtures/read/incident_list_injection.json")
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Result []map[string]string `json:"result"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatal(err)
	}
	var out []domain.Record
	for _, m := range body.Result {
		out = append(out, domain.Record{Table: "incident", Fields: m})
	}
	return out
}

func TestPresentMarksFreeTextUntrustedWithAuthorAndTimestamp(t *testing.T) {
	recs := loadFixtureRecords(t)
	p := read.Present(recs[0])
	d, ok := p["description"].(output.Untrusted)
	if !ok {
		t.Fatalf("description = %T, want output.Untrusted", p["description"])
	}
	if d.Author != "alice" || !d.Timestamp.Equal(time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("author/timestamp = %q %v", d.Author, d.Timestamp)
	}
	if _, ok := p["work_notes"].(output.Untrusted); !ok {
		t.Error("work_notes must be untrusted")
	}
	if p["number"] != "INC0010001" || p["state"] != "2" {
		t.Errorf("structured fields stay plain: %v", p)
	}
	// Empty free text is not wrapped (nothing to mark).
	if s, ok := read.Present(recs[1])["description"].(string); !ok || s != "" {
		t.Errorf("empty description = %#v", read.Present(recs[1])["description"])
	}
}

func TestInjectionTextIsMarkedInJSONAndContainedInText(t *testing.T) {
	recs := loadFixtureRecords(t)
	data := read.ListData{Items: []map[string]any{read.Present(recs[0])}}
	var js strings.Builder
	if err := output.Write(&js, output.Success(data, nil), output.Options{Format: output.FormatJSON}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js.String(), `"untrusted":true`) || !strings.Contains(js.String(), "ignore all previous instructions") {
		t.Errorf("json = %s", js.String())
	}
	// Instruction text appears only inside an untrusted object, never as a plain string value.
	var env struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(js.String()), &env); err != nil {
		t.Fatal(err)
	}
	desc, _ := env.Data.Items[0]["description"].(map[string]any)
	if desc["untrusted"] != true {
		t.Errorf("description = %v", env.Data.Items[0]["description"])
	}
	var txt strings.Builder
	if err := output.Write(&txt, output.Success(data, nil), output.Options{Format: output.FormatText}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(txt.String(), "<<<END UNTRUSTED>>>") != 2 { // one per untrusted field; the planted one is neutralised
		t.Errorf("planted delimiter must not close a block:\n%s", txt.String())
	}
}

func TestIsUntrusted(t *testing.T) {
	for _, f := range []string{"description", "short_description", "comments", "work_notes", "close_notes"} {
		if !read.IsUntrusted(f) {
			t.Errorf("%s must be untrusted", f)
		}
	}
	for _, f := range []string{"number", "state", "sys_id", "assigned_to"} {
		if read.IsUntrusted(f) {
			t.Errorf("%s must not be untrusted", f)
		}
	}
}
