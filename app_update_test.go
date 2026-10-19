package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The swap script used to delete itself, and a batch file that deletes itself
// is read line by line: the line after the deletion cannot be read any more,
// and the shell answers "the batch file cannot be found" — a complaint in a
// window on the reader's screen, over a swap that has already worked.
//
// The script must therefore not delete itself, and every path through it must
// end in an explicit exit.
func TestUpdateSwapScriptDoesNotDeleteItself(t *testing.T) {
	s := updateSwapScript(`C:\temp\eagle-update-1.exe`, `C:\app\Eagle.exe`)
	if strings.Contains(s, "del \"%~f0\"") {
		t.Error("the swap script deletes itself; the shell cannot read the line after that")
	}
	if !strings.Contains(s, "exit /b") {
		t.Error("the swap script does not end in an explicit exit")
	}
	// The parts that do the work must still be there.
	for _, want := range []string{
		`set "NEW=C:\temp\eagle-update-1.exe"`,
		`set "SELF=C:\app\Eagle.exe"`,
		`move /y "%NEW%" "%SELF%"`,
		`start "" "%SELF%"`,
		"ping -n 2 127.0.0.1 > nul",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the swap script is missing %q", want)
		}
	}
}

// canReplace answers whether this build's own folder can be written to. The
// swap moves the new build over the running one, and a folder the reader
// cannot write to would fail every try — the reader would be told an update
// was installing and end up with the old build.
func TestCanReplace(t *testing.T) {
	dir := t.TempDir()
	if !canReplace(dir) {
		t.Error("canReplace said no to a folder the test just created")
	}
	// A folder that is not there cannot be written to either.
	if canReplace(filepath.Join(dir, "no-such-folder")) {
		t.Error("canReplace said yes to a folder that does not exist")
	}
	// The check must not leave anything behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("canReplace left %d file(s) behind", len(entries))
	}
}
