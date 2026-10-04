package config

import "testing"

func TestSelftestFixturesParse(t *testing.T) {
	c, err := Parse([]byte("profiles:\n  p:\n    mode: agent\n    instance: {host: a.example.com}\n    selftest: {fixture_incident: INC1, foreign_incident: INC2}\n"))
	if err != nil {
		t.Fatal(err)
	}
	s := c.Profiles["p"].Selftest
	if s.FixtureIncident != "INC1" || s.ForeignIncident != "INC2" {
		t.Errorf("%+v", s)
	}
	if _, err := Parse([]byte("profiles:\n  p:\n    mode: agent\n    instance: {host: a.example.com}\n    selftest: {nope: 1}\n")); err == nil {
		t.Error("unknown selftest key must be rejected")
	}
}
