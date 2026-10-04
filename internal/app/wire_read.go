package app

import "github.com/stainedhead/snow-cli/internal/sn"

// wireRead attaches the read-path ports (Tables, Catalog) to the Env.
// Owned by WS-B.
func wireRead(w *Wiring) {
	w.Env.Tables = sn.NewTables(w.Client)
	w.Env.Catalog = sn.NewCatalog(w.Client)
}
