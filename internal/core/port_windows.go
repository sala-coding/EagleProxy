//go:build windows

package core

import (
	"os/exec"
	"strconv"
	"strings"

	"eagle/internal/util"
)

// netstatRows returns the individual rows of `netstat -ano -p tcp`, trimmed.
//
// There is no portable way from a port to its owner, and that asymmetry is the
// whole reason a port conflict could only ever be described as "some core from
// last time": the app knows about the processes it started and nothing about
// anything else. netstat is the cheap way out — one hidden console process, no
// parsing of the socket table by hand.
func netstatRows() []string {
	cmd := exec.Command("netstat", "-ano", "-p", "tcp")
	util.HideConsole(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var rows []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			rows = append(rows, line)
		}
	}
	return rows
}

// listenersOnPort maps a local TCP port back to the processes listening on it.
func listenersOnPort(port int) []int {
	if port <= 0 || port > 65535 {
		return nil
	}
	want := ":" + strconv.Itoa(port)
	var pids []int
	seen := map[int]bool{}
	for _, line := range netstatRows() {
		fields := strings.Fields(line)
		// Proto  LocalAddress  ForeignAddress  State  PID
		if len(fields) < 4 {
			continue
		}
		if !strings.EqualFold(fields[3], "LISTENING") || !strings.HasSuffix(fields[1], want) {
			continue
		}
		pid, err := strconv.Atoi(fields[len(fields)-1])
		if err != nil || seen[pid] {
			continue
		}
		seen[pid] = true
		pids = append(pids, pid)
	}
	return pids
}

// portLines returns the raw rows mentioning a local port, whatever state they
// are in. When a bind is refused but nothing is listening, these rows are the
// only evidence of why: a TIME_WAIT socket shows up here, and an empty result
// points at a reserved port range instead — the one case where there is no
// process at all and "end the other program" is advice that cannot be followed.
func portLines(port int) []string {
	if port <= 0 || port > 65535 {
		return nil
	}
	want := ":" + strconv.Itoa(port)
	var rows []string
	for _, line := range netstatRows() {
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.HasSuffix(fields[1], want) {
			rows = append(rows, line)
		}
	}
	return rows
}
