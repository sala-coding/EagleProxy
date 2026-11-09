package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"eagle/internal/vpn"
)

// uwpExemptScript is the one elevated run behind the UWP switch: every installed
// package framework is added to — or taken out of — the loopback exemption store. The
// store is machine-wide, which is why this needs the admin token, and CheckNetIsolation
// is its only documented door. Frameworks rather than apps: the isolation applies to
// the framework a Store app runs its network code in, so exempting the frameworks
// covers the apps, including the ones installed later.
const uwpExemptScript = `$ErrorActionPreference = 'Stop'
$list = Get-AppxPackage -PackageTypeFilter Framework | Select-Object -ExpandProperty PackageFamilyName
foreach ($p in $list) {
  CheckNetIsolation.exe LoopbackExempt %s -n="$p" | Out-Null
}
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
// elevation for the whole run: the script is written to a temp file and started
// elevated, and its exit code is the answer — a dismissed UAC prompt and a refused
// package both come back as "not done" instead of a switch that moved on its own.
func (a *App) SetUWPLoopback(on bool) error {
	verb := "-a"
	if !on {
		verb = "-d"
	}
	dir, err := os.MkdirTemp("", "eagle-uwp")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "uwp.ps1")
	if err := os.WriteFile(path, []byte(fmt.Sprintf(uwpExemptScript, verb)), 0o644); err != nil {
		return err
	}
	code, err := vpn.RunElevatedWait("powershell.exe", []string{
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", path,
	}, dir)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("UWP 回环解锁失败（退出码 %d）", code)
	}
	a.writeUWPLoopback(on)
	return nil
}
