package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The sweep takes what EARLIER runs left behind: the names carry the pid of the run that
// wrote them, and the run that is starting has a different one, so a filter built from the
// current pid matched nothing an earlier update had left — the script of a finished update
// stayed in %TEMP% for good. Its own names are skipped (a retry inside one process leaves
// them), and anything that is not one of this app's .bat/.exe temp files is left alone.
func TestCleanupUpdateTempsTakesEarlierRuns(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"eagle-update-101.exe",
		"eagle-update-101.bat",
		"eagle-update-202.exe",
		"eagle-update-202.bat",
		"eagle-update-other.txt",
		"not-eagle-update-303.bat",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("TEMP", dir)
	t.Setenv("TMP", dir)

	cleanupUpdateTemps(202)

	for _, name := range []string{"eagle-update-101.exe", "eagle-update-101.bat"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s was left behind or returned unexpected error: %v", name, err)
		}
	}
	for _, name := range []string{
		"eagle-update-202.exe",
		"eagle-update-202.bat",
		"eagle-update-other.txt",
		"not-eagle-update-303.bat",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was removed: %v", name, err)
		}
	}
}
