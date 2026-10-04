package app

import (
	"github.com/stainedhead/snow-cli/internal/cli"
	"github.com/stainedhead/snow-cli/internal/usecase/selftest"
)

// wireSelftest hands the selftest use case the merged ports (read, write and
// identity). It runs after wireRead and wireWrite.
func wireSelftest(w *Wiring) {
	e := w.Env
	e.Extra[cli.ExtraSelftest] = selftest.Service{
		Guard: e.Guard, Tables: e.Tables, Catalog: e.Catalog, Identity: e.Identity,
		Incidents: e.Incidents, Mode: e.Mode,
		Fixture: selftest.Fixture{Own: w.Profile.Selftest.FixtureIncident, Foreign: w.Profile.Selftest.ForeignIncident},
	}
}
