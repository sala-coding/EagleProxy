//go:build windows

package tunserv

import (
	"bytes"
	"os/exec"
	"strings"

	"eagle/internal/util"
)

// execute runs a console tool the way every other helper in this app does:
// console subsystem means a window unless the console is hidden first.
func execute(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	util.HideConsole(cmd)
	return cmd.Run()
}

// captureQuery runs the /Query step and returns what task list it saw. A
// missing task shows as a non-zero run with real text on stdout, so the
// answer is read out rather than guessed from the exit code.
func captureQuery(exePath string) (string, error) {
	cmd := exec.Command("schtasks.exe", "/Query", "/FO", "LIST", "/TN", TaskName(exePath))
	util.HideConsole(cmd)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// containsTask answers whether the queried listing names the task. The task
// name travels with the listing when it exists (the "TaskName" row), and a
// missing listing is the word of error text instead — comparison on the whole
// lowered text keeps both shapes from reading as one.
func containsTask(text, task string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(task))
}
