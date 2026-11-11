package configbuilder

import (
	"testing"

	"eagle/internal/setting"
)

// The stack field is what a 1.15 core refuses to start on: naming any of the three
// answers in a config that core reads ends as "gVisor is not included in this build".
// The number in the settings file is written by whatever core was configured at the
// time, so the value the reader had before the switch must not survive it.
func TestTunFrontOmitsStackOn115(t *testing.T) {
	s := setting.Load(t.TempDir())
	s.InboundSocksPort = 2080
	s.VPNImplementation = 2 // mixed: what a file written for a 1.14 core holds

	in := func() map[string]interface{} {
		return BuildTunFront(s)["inbounds"].([]interface{})[0].(map[string]interface{})
	}

	SetCoreVersion("1.14.2")
	if in()["stack"] != "mixed" {
		t.Fatalf("1.14: want stack=mixed, got %v", in()["stack"])
	}

	SetCoreVersion("1.15.0-alpha.13")
	if v, ok := in()["stack"]; ok {
		t.Fatalf("1.15: the field must be left out, got %v", v)
	}
}
