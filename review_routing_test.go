package main

import (
	"testing"

	"eagle/internal/setting"
)

// The routing profile the app runs on is the in-memory copy (Setting.Routing),
// not the file:
//
//   - App.syncDNSStub reads a.curSetting().Routing() for the forwarder's two
//     upstreams (the DNS card's smart pick is supposed to move them),
//   - configbuilder.BuildConfig reads b.s.Routing() for the core's rules.
//
// App.SaveRouting (app_settings.go) writes the file through Setting.SaveRouting,
// which never refreshes that in-memory copy. So after a routing save the app keeps
// running the previous routing while the file (and the settings card, which reads
// the file) shows the new one.
//
// Both tests here were written as demonstrations first — they reproduced the
// behaviour and failed, naming the line that was wrong — and they now guard the
// fix: the production file was corrected and the test stayed behind.
//
//	cd e:\project\eagle-desk; go test -run TestReview ./

// reviewRoutingFixture builds an App whose settings live in a throwaway data
// directory, so a routing save can be exercised without touching the real one.
func reviewRoutingFixture(t *testing.T) (*App, *setting.Setting) {
	t.Helper()
	dir := t.TempDir()
	s := setting.Load(dir)
	if s == nil {
		t.Fatal("setting.Load returned nil")
	}
	a := &App{}
	a.setDataDir(dir, dir)
	a.setSetting(s)
	if a.curSetting().Routing() == nil {
		t.Fatal("the active routing is nil")
	}
	return a, s
}

// TestReviewSaveRoutingUpdatesTheLiveRouting pins the state the app actually
// runs on: a saved routing has to be the one the forwarder and the core are
// configured from, not only the one on disk.
func TestReviewSaveRoutingUpdatesTheLiveRouting(t *testing.T) {
	a, s := reviewRoutingFixture(t)

	live := a.curSetting().Routing()
	edited := *live
	edited.RemoteDNS = "https://dns.example.test/dns-query"
	edited.DirectDNS = "https://direct.example.test/dns-query"

	if err := a.SaveRouting(s.ActiveRouting, edited); err != nil {
		t.Fatalf("App.SaveRouting: %v", err)
	}

	// The file has it.
	onDisk, err := setting.LoadRouting(a.curDataDir(), s.ActiveRouting)
	if err != nil {
		t.Fatalf("setting.LoadRouting: %v", err)
	}
	if onDisk.RemoteDNS != edited.RemoteDNS {
		t.Fatalf("the routing file was not written: on-disk RemoteDNS=%q", onDisk.RemoteDNS)
	}

	// The live copy has to have it too — that is the one the forwarder and the
	// core are actually configured from.
	got := a.curSetting().Routing()
	if got.RemoteDNS != edited.RemoteDNS {
		t.Errorf("live routing is stale after SaveRouting: syncDNSStub/configbuilder would still use RemoteDNS=%q, want %q",
			got.RemoteDNS, edited.RemoteDNS)
	}
	if got.DirectDNS != edited.DirectDNS {
		t.Errorf("live routing is stale after SaveRouting: syncDNSStub/configbuilder would still use DirectDNS=%q, want %q",
			got.DirectDNS, edited.DirectDNS)
	}
}

// TestReviewApplyRoutingUpdatesTheLiveRouting is the same defect on the path
// that is supposed to put a routing change into force at once: App.ApplyRouting
// saves the file and then rebuilds the core — but the core is built from
// a.curSetting(), whose routing is still the old one, so the rebuilt core carries
// the previous rules while the toast says the routing was updated.
func TestReviewApplyRoutingUpdatesTheLiveRouting(t *testing.T) {
	a, s := reviewRoutingFixture(t)

	live := a.curSetting().Routing()
	edited := *live
	edited.RemoteDNS = "https://dns2.example.test/dns-query"
	edited.DefOutbound = "direct"

	// No node is running, so ApplyRouting returns right after syncDNSStub.
	if err := a.ApplyRouting(s.ActiveRouting, edited); err != nil {
		t.Fatalf("App.ApplyRouting: %v", err)
	}

	onDisk, err := setting.LoadRouting(a.curDataDir(), s.ActiveRouting)
	if err != nil {
		t.Fatalf("setting.LoadRouting: %v", err)
	}
	if onDisk.DefOutbound != "direct" {
		t.Fatalf("the routing file was not written: on-disk DefOutbound=%q", onDisk.DefOutbound)
	}

	got := a.curSetting().Routing()
	if got.RemoteDNS != edited.RemoteDNS || got.DefOutbound != edited.DefOutbound {
		t.Errorf("live routing is stale after ApplyRouting: the rebuilt core would carry RemoteDNS=%q DefOutbound=%q, want %q/%q",
			got.RemoteDNS, got.DefOutbound, edited.RemoteDNS, edited.DefOutbound)
	}
}
