package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupUpdateTempsOnlyRemovesRequestedPID(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"eagle-update-101.exe",
		"eagle-update-101.bat",
		"eagle-update-202.exe",
		"eagle-update-202.bat",
		"eagle-update-other.txt",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("TEMP", dir)
	t.Setenv("TMP", dir)

	cleanupUpdateTemps(101)

	for _, name := range []string{"eagle-update-101.exe", "eagle-update-101.bat"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s still exists or returned unexpected error: %v", name, err)
		}
	}
	for _, name := range []string{"eagle-update-202.exe", "eagle-update-202.bat", "eagle-update-other.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was removed: %v", name, err)
		}
	}
}
