//go:build windows

package sysproxy_test

import (
	. "eagle/internal/sysproxy"

	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

const reviewInternetSettings = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// reviewSnapshot is the machine's proxy setting as it was found, so that the test can
// put it back exactly as it was.
type reviewSnapshot struct {
	enable    uint32
	hasEnable bool
	server    string
	hasServer bool
	override  string
	hasOver   bool
	connBlob  []byte
	hasConn   bool
}

func reviewRead(t *testing.T) reviewSnapshot {
	t.Helper()
	var s reviewSnapshot
	k, err := registry.OpenKey(registry.CURRENT_USER, reviewInternetSettings, registry.QUERY_VALUE)
	if err != nil {
		t.Fatalf("open internet settings: %v", err)
	}
	defer k.Close()
	if v, _, err := k.GetIntegerValue("ProxyEnable"); err == nil {
		s.enable, s.hasEnable = uint32(v), true
	}
	if v, _, err := k.GetStringValue("ProxyServer"); err == nil {
		s.server, s.hasServer = v, true
	}
	if v, _, err := k.GetStringValue("ProxyOverride"); err == nil {
		s.override, s.hasOver = v, true
	}
	ck, err := registry.OpenKey(registry.CURRENT_USER, reviewInternetSettings+`\Connections`, registry.QUERY_VALUE)
	if err == nil {
		defer ck.Close()
		buf := make([]byte, 8192)
		if n, _, err := ck.GetValue("DefaultConnectionSettings", buf); err == nil && n > 0 {
			s.connBlob, s.hasConn = buf[:n], true
		}
	}
	return s
}

func reviewRestore(t *testing.T, s reviewSnapshot) {
	t.Helper()
	k, err := registry.OpenKey(registry.CURRENT_USER, reviewInternetSettings, registry.SET_VALUE)
	if err != nil {
		t.Errorf("open internet settings for restore: %v", err)
		return
	}
	defer k.Close()
	if s.hasEnable {
		if err := k.SetDWordValue("ProxyEnable", s.enable); err != nil {
			t.Errorf("restore ProxyEnable: %v", err)
		}
	} else {
		_ = k.DeleteValue("ProxyEnable")
	}
	if s.hasServer {
		if err := k.SetStringValue("ProxyServer", s.server); err != nil {
			t.Errorf("restore ProxyServer: %v", err)
		}
	} else {
		_ = k.DeleteValue("ProxyServer")
	}
	if s.hasOver {
		if err := k.SetStringValue("ProxyOverride", s.override); err != nil {
			t.Errorf("restore ProxyOverride: %v", err)
		}
	} else {
		_ = k.DeleteValue("ProxyOverride")
	}
	ck, err := registry.OpenKey(registry.CURRENT_USER, reviewInternetSettings+`\Connections`, registry.SET_VALUE)
	if err == nil {
		defer ck.Close()
		if s.hasConn {
			if err := ck.SetBinaryValue("DefaultConnectionSettings", s.connBlob); err != nil {
				t.Errorf("restore DefaultConnectionSettings: %v", err)
			}
		} else {
			_ = ck.DeleteValue("DefaultConnectionSettings")
		}
	}
}

// TestReviewOriginalProxyIsSavedAndRestored is the whole of the system proxy's
// exit story: the machine's own proxy is overwritten when the app takes over —
// that is what taking over means — and put back when it lets go.
//
// Set records the original first (the undo file, see sysproxy.SetUndoFile);
// Clear restores that record rather than writing the DIRECT flag. This test
// used to pin the opposite defect (overwritten and never put back, with both
// branches of its first assertion failing on purpose); it now asserts the
// fixed contract.
//
// The test writes a recognisable "original" of its own, so the machine's own setting
// is not what is being measured, and puts the real one back afterwards.
//
// It is skipped unless EAGLE_REVIEW_PROXY=1 is set, because it changes a live
// machine setting.
func TestReviewOriginalProxyIsSavedAndRestored(t *testing.T) {
	if os.Getenv("EAGLE_REVIEW_PROXY") != "1" {
		t.Skip("set EAGLE_REVIEW_PROXY=1 to run: this changes the machine's system proxy")
	}
	saved := reviewRead(t)
	t.Cleanup(func() { reviewRestore(t, saved) })
	// A record of its own, so the test never reads or removes a real takeover's.
	SetUndoFile(filepath.Join(t.TempDir(), "sysproxy-undo.json"))

	const original = "corp-proxy.invalid:8080"
	k, err := registry.OpenKey(registry.CURRENT_USER, reviewInternetSettings, registry.SET_VALUE)
	if err != nil {
		t.Fatalf("open internet settings: %v", err)
	}
	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		k.Close()
		t.Fatalf("write ProxyEnable: %v", err)
	}
	if err := k.SetStringValue("ProxyServer", original); err != nil {
		k.Close()
		t.Fatalf("write ProxyServer: %v", err)
	}
	if err := k.SetStringValue("ProxyOverride", "<local>;*.corp.invalid"); err != nil {
		k.Close()
		t.Fatalf("write ProxyOverride: %v", err)
	}
	k.Close()

	// The app turning its system proxy on: the takeover lands on the machine.
	if err := Set("{ip}:{http_port}", "127.0.0.1", 2080, 2080, ""); err != nil {
		t.Fatalf("Set: %v", err)
	}
	afterSet := reviewRead(t)
	t.Logf("after Set:  ProxyEnable=%v ProxyServer=%q ProxyOverride=%q",
		afterSet.enable, afterSet.server, afterSet.override)
	if !strings.Contains(afterSet.server, "127.0.0.1:2080") {
		t.Errorf("Set did not point the machine at the app: ProxyServer=%q", afterSet.server)
	}
	if !HasUndo() {
		t.Error("Set recorded no undo: the original is already lost")
	}

	// The app turning it off again (its exit path does exactly this): the
	// original comes back, bypass list included.
	if err := Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	afterClear := reviewRead(t)
	t.Logf("after Clear: ProxyEnable=%v ProxyServer=%q ProxyOverride=%q",
		afterClear.enable, afterClear.server, afterClear.override)
	if !strings.Contains(afterClear.server, original) {
		t.Errorf("the original proxy was NOT restored: ProxyServer=%q ProxyEnable=%v",
			afterClear.server, afterClear.enable)
	}
	if afterClear.override != "<local>;*.corp.invalid" {
		t.Errorf("the original bypass list was NOT restored: ProxyOverride=%q", afterClear.override)
	}
	if HasUndo() {
		t.Error("the undo record survived its own restore")
	}
}
