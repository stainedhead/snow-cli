package read

import (
	"sort"

	"github.com/stainedhead/agent-cli-core/output"
)

// Fit trims d.Items to the largest prefix whose rendered envelope fits
// maxBytes in the given format (0 means the core default), and returns the
// matching meta (count). Core truncation only cuts top-level arrays and
// strings, not an object holding an items array (open check R-04), so snow
// trims here. A trimmed list sets Truncated and points page.next_offset at
// the first item that was dropped, as an absolute sysparm_offset. A list
// that cannot fit even with one item is returned unchanged so the core
// reports the bound as too small.
func Fit(d ListData, format output.Format, maxBytes int) (ListData, *output.Meta) {
	if maxBytes <= 0 {
		maxBytes = output.DefaultMaxBytes
	}
	fits := func(x ListData) bool {
		b, err := output.Render(output.Success(x, &output.Meta{Count: len(x.Items)}),
			output.Options{Format: format, Bounds: output.Bounds{MaxBytes: maxBytes}})
		return err == nil && len(b) <= maxBytes
	}
	trim := func(n int) ListData {
		x := d
		x.Items = d.Items[:n]
		x.Page.Returned = n
		next := d.Page.Offset + n
		x.Page.NextOffset = &next
		x.Truncated = true
		return x
	}
	if fits(d) || len(d.Items) < 2 {
		return d, &output.Meta{Count: len(d.Items)}
	}
	// Largest n in [1, len-1] that fits; rendered size grows with n.
	n := sort.Search(len(d.Items)-1, func(i int) bool { return !fits(trim(i + 1)) })
	if n == 0 {
		return d, &output.Meta{Count: len(d.Items)}
	}
	out := trim(n)
	return out, &output.Meta{Count: n}
}
