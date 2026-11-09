package configbuilder

import (
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
)

// The version of the sing-box core this process runs, as the host told
// configbuilder (see app_desktop's probe). Empty until the first probe lands —
// which the tun stack decision reads as "a core the native stack question is
// closed on".
var coreVersion atomic.Value // string

// SetCoreVersion records what core the host runs. The config builder asks on
// every build — it cannot probe itself without reaching the launcher's core
// resolution, so the host hands the answer over.
func SetCoreVersion(v string) {
	coreVersion.Store(strings.TrimSpace(v))
}

// CoreVersion is what was recorded, empty when nothing was probed yet.
func CoreVersion() string {
	v, _ := coreVersion.Load().(string)
	return v
}

// atLeast reports whether the recorded core is major.minor or newer. A core that
// cannot be probed, or one whose answer does not parse, reads as older — the safe
// side is the side that still writes the fields the old core understands.
func atLeast(major, minor int) bool {
	m := regexp.MustCompile(`^(\d+)\.(\d+)`).FindStringSubmatch(CoreVersion())
	if m == nil {
		return false
	}
	gotMajor, err := strconv.Atoi(m[1])
	if err != nil {
		return false
	}
	if gotMajor != major {
		return gotMajor > major
	}
	gotMinor, err := strconv.Atoi(m[2])
	if err != nil {
		return false
	}
	return gotMinor >= minor
}

// NativeStackSupported reports whether the recorded core is sing-box 1.15.0 or
// newer: sing-tun ships its own TCP/IP stack from there on, the stack field is
// deprecated and the reader's choice between gvisor/system/mixed stops applying.
func NativeStackSupported() bool { return atLeast(1, 15) }

// HttpClientSupported reports whether the recorded core is sing-box 1.14.0 or
// newer: from there a remote rule-set names the HTTP client it is downloaded
// with, and the implicit default — a download through the default outbound — is
// deprecated. A core of 1.15 refuses it outright unless an environment variable
// turns the old behaviour back on, which is the FATAL in the log ("核心反复退出"
// three times over).
func HttpClientSupported() bool { return atLeast(1, 14) }
