package read_test

import (
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase/read"
)

func bigList(n, offset int) read.ListData {
	items := make([]map[string]any, n)
	for i := range items {
		items[i] = map[string]any{"number": "INC", "short_description": strings.Repeat("x", 200)}
	}
	tot := 1000
	return read.ListData{Items: items, Page: domain.NewPage(offset, n, n, &tot)}
}

func TestFitLeavesSmallListsAlone(t *testing.T) {
	d := bigList(2, 0)
	got, meta := read.Fit(d, output.FormatJSON, 0)
	if got.Truncated || len(got.Items) != 2 || meta.Count != 2 || meta.Truncated {
		t.Errorf("got %+v meta %+v", got, meta)
	}
}

func TestFitTrimsItemsToMaxBytes(t *testing.T) {
	// R-04: core only truncates top-level arrays, so snow trims items itself.
	d := bigList(20, 40)
	got, meta := read.Fit(d, output.FormatJSON, 1500)
	if !got.Truncated || len(got.Items) == 0 || len(got.Items) >= 20 {
		t.Fatalf("kept %d, truncated=%v", len(got.Items), got.Truncated)
	}
	kept := len(got.Items)
	if got.Page.Returned != kept || got.Page.NextOffset == nil || *got.Page.NextOffset != 40+kept {
		t.Errorf("page = %+v", got.Page)
	}
	if meta.Count != kept {
		t.Errorf("meta.count = %d", meta.Count)
	}
	b, err := output.Render(output.Success(got, meta), output.Options{Format: output.FormatJSON, Bounds: output.Bounds{MaxBytes: 1500}})
	if err != nil || len(b) > 1500 {
		t.Fatalf("rendered %d bytes, err %v", len(b), err)
	}
	// One more item would not have fit.
	more := d
	more.Items = d.Items[:kept+1]
	if b, err := output.Render(output.Success(more, &output.Meta{Count: kept + 1}), output.Options{Format: output.FormatJSON, Bounds: output.Bounds{MaxBytes: 1500}}); err == nil && len(b) <= 1500 {
		t.Error("Fit must keep the largest prefix that fits")
	}
}

func TestFitUnfittableIsLeftForCoreToReport(t *testing.T) {
	d := bigList(3, 0)
	got, _ := read.Fit(d, output.FormatJSON, 50)
	if len(got.Items) != 3 || got.Truncated {
		t.Errorf("an unfittable list must be returned unchanged, got %d items", len(got.Items))
	}
}

func TestFitWorksForTableFormat(t *testing.T) {
	got, _ := read.Fit(bigList(20, 0), output.FormatTable, 1500)
	if !got.Truncated {
		t.Error("table format must truncate too")
	}
}
