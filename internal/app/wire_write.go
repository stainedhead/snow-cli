package app

import (
	"github.com/stainedhead/snow-cli/internal/cli"
	"github.com/stainedhead/snow-cli/internal/sn"
)

// wireWrite attaches the write-path ports (Incidents, Tasks, Orders) to the
// Env. Owned by WS-C. The catalog reader used for order validation is wired
// by wireRead (WS-B).
func wireWrite(w *Wiring) {
	inc := w.Profile.Incident
	w.Env.Incidents = sn.NewIncidentAdapter(w.Client, sn.IncidentOptions{
		CreateVia: inc.CreateVia, Producer: inc.Producer, ResolvedState: inc.States["resolved"],
	})
	tasks := sn.NewTaskAdapter(w.Client)
	w.Env.Tasks = tasks
	w.Env.Orders = sn.NewOrderAdapter(w.Client)
	if w.Env.Extra == nil {
		w.Env.Extra = map[string]any{}
	}
	w.Env.Extra[cli.ExtraTaskFetcher] = tasks
}
