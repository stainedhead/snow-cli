package cli

// RegisterAll registers every area. Streams never edit this file: each adds
// its commands inside its own Register<Area> function (cmd_<area>.go).
func RegisterAll(r *Router) {
	RegisterCore(r)
	RegisterRead(r)
	RegisterWrite(r)
	RegisterAuth(r)
	RegisterSelftest(r)
	RegisterSkill(r)
}
