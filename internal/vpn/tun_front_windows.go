//go:build windows

package vpn

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"eagle/internal/util"
)

// listTunFronts returns the pids of every process started from frontCfg — the fixed
// config the elevated TUN front-end runs (see configbuilder.BuildTunFront).
//
// The config path is the whole criterion. Nothing else in the app is launched with
// it, and a front-end that outlived the app it belonged to still carries it in its
// command line — which is the only thing left to recognise it by, since the pid it
// was recorded under lived in the memory of a process that is gone.
//
// Contains rather than a wildcard match: the path goes into a PowerShell string, and
// a wildcard would make a perfectly ordinary character in a directory name mean
// something. The match is exact, character for character, which is what a path
// deserves.
func listTunFronts(frontCfg string) ([]int, error) {
	if frontCfg == "" {
		return nil, nil
	}
	script := fmt.Sprintf(
		`@(Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Where-Object { $_.CommandLine -and $_.CommandLine.Contains('%s') } | ForEach-Object { $_.ProcessId }) -join ','`,
		psEscape(frontCfg))
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	util.HideConsole(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("查找残留 TUN 前端失败: %w", err)
	}
	return parsePids(string(out)), nil
}

// StopOrphanTunFronts terminates every front-end started from frontCfg except keep,
// and reports how many it stopped.
//
// This is the net under the pid the app passes around. Left alone, a front-end that
// nobody is tracking keeps EagleTun up, and the next front-end cannot have it — the
// adapter is there but the name is taken, which surfaces as a start that dies on its
// own config and a switch that appears to do nothing.
func StopOrphanTunFronts(frontCfg string, keep int) (int, error) {
	pids, err := listTunFronts(frontCfg)
	if err != nil {
		return 0, err
	}
	var orphans []int
	for _, p := range pids {
		if p != keep {
			orphans = append(orphans, p)
		}
	}
	if len(orphans) == 0 {
		return 0, nil
	}
	if err := StopElevatedMany(orphans); err != nil {
		// Report what actually went rather than what was asked for: a refused UAC
		// leaves the adapter held, and counting those as stopped would hide the one
		// fact the caller has to act on.
		stopped := 0
		for _, p := range orphans {
			if !IsRunning(p) {
				stopped++
			}
		}
		return stopped, err
	}
	return len(orphans), nil
}

// parsePids reads the comma-joined pid list the lookup script prints. Anything that
// is not a number is dropped rather than guessed at: a pid is not a thing to invent.
func parsePids(s string) []int {
	var pids []int
	for _, field := range strings.Split(strings.TrimSpace(s), ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if n, err := strconv.Atoi(field); err == nil && n > 0 {
			pids = append(pids, n)
		}
	}
	return pids
}
