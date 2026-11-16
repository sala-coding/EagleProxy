package main

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"eagle/internal/vpn"
)

// uwpExemptScript is the one elevated run behind the UWP switch: every installed
// package framework is added to — or taken out of — the loopback exemption store. The
// store is machine-wide, which is why this needs the admin token, and CheckNetIsolation
// is its only documented door. Frameworks rather than apps: the isolation applies to
// the framework a Store app runs its network code in, so exempting the frameworks
// covers the apps, including the ones installed later.
const uwpExemptScript = `$ErrorActionPreference = 'Stop'
$bad = 0
$list = Get-AppxPackage -PackageTypeFilter Framework | Select-Object -ExpandProperty PackageFamilyName
foreach ($p in $list) {
  CheckNetIsolation.exe LoopbackExempt %s -n="$p" | Out-Null
  if ($LASTEXITCODE -ne 0) { $bad++ }
}
if ($bad -gt 0) { exit 1 }
`

// uwpStateName is the note this app keeps of its own switch. The store cannot be asked
// reliably from inside the window: the listing is readable, but Get-AppxPackage — which
// says which frameworks are installed — answers empty in a windowless process, so
// "every framework is exempt" read as false even after a successful unlock (the switch
// fell back to off every time it was turned on). Which machine is unlocked is a fact
// this app changed, so it is written down here, beside the rule sets it also keeps.
const uwpStateName = "uwp-loopback.txt"

func (a *App) uwpStatePath() string {
	return filepath.Join(a.curDataDir(), uwpStateName)
}

// UWPLoopback reports whether this app last unlocked the store successfully.
func (a *App) UWPLoopback() bool {
	data, err := os.ReadFile(a.uwpStatePath())
	return err == nil && strings.TrimSpace(string(data)) == "on"
}

func (a *App) writeUWPLoopback(on bool) {
	state := "off"
	if on {
		state = "on"
	}
	_ = os.WriteFile(a.uwpStatePath(), []byte(state), 0o644)
}

// SetUWPLoopback unlocks (on) or re-locks every installed package framework. One
// elevation for the whole run, and its exit code is the answer: the script exits
// non-zero when CheckNetIsolation refuses any package, so a refusal is "not done"
// rather than a switch that moved on its own on a machine where nothing changed.
func (a *App) SetUWPLoopback(on bool) error {
	verb := "-a"
	if !on {
		verb = "-d"
	}
	// Nothing is written to disk. The script used to be a file in a temp directory this
	// user can write, handed to an elevated PowerShell between the write and the read —
	// a window in which any process running as the same user could put its own script
	// behind the admin token. An argument cannot be swapped after the fact.
	code, err := vpn.RunElevatedWait("powershell.exe", []string{
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", pwshEncoded(fmt.Sprintf(uwpExemptScript, verb)),
	}, "")
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("UWP 回环解锁失败（退出码 %d，CheckNetIsolation 拒绝了部分包）", code)
	}
	a.writeUWPLoopback(on)
	return nil
}

// pwshEncoded is what -EncodedCommand reads: the script as UTF-16LE, base64.
func pwshEncoded(script string) string {
	units := utf16.Encode([]rune(script))
	buf := make([]byte, len(units)*2)
	for i, u := range units {
		binary.LittleEndian.PutUint16(buf[i*2:], u)
	}
	return base64.StdEncoding.EncodeToString(buf)
}
