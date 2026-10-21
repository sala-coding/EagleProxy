package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The logs go into one folder, beside the executable.
//
// One folder, because a reader looking for "the log" should find one place and
// not five: the categorised text logs, the core's own log, the crash record and
// the resident server's all sit in it.
func TestReviewLogDirIsBesideTheExecutable(t *testing.T) {
	exe := t.TempDir()
	data := t.TempDir()

	dir := resolveLogDir(exe, data)
	if want := filepath.Join(exe, "log"); dir != want {
		t.Errorf("log dir = %q, want %q", dir, want)
	}
	// And it is there: a folder that appears only when something is written to it
	// is a folder the reader does not find.
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("the log folder was not created: %v", err)
	}
	if !st.IsDir() {
		t.Errorf("%q is not a folder", dir)
	}
}

// A program folder that cannot be written to falls back to the data directory.
//
// A log that cannot be written is no log at all, and the program's own folder is
// not always writable — an installed copy under Program Files, a folder on a
// read-only share. The fallback is the other place this app is allowed to write.
func TestReviewLogDirFallsBackWhenTheProgramFolderRefuses(t *testing.T) {
	// A file where the folder should be: nothing can be created inside it.
	base := t.TempDir()
	blocked := filepath.Join(base, "blocked")
	if err := os.WriteFile(blocked, []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()

	dir := resolveLogDir(blocked, data)
	if want := filepath.Join(data, "log"); dir != want {
		t.Errorf("log dir = %q, want the data directory's %q", dir, want)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Errorf("the fallback folder was not created: %v", err)
	}
}

// The logs this app used to write in other places come along.
//
// They are moved, not copied: a log in two places is a log nobody knows to look
// at. The categorised logs keep their names on the way, because the log panel
// reads them by category and day.
func TestReviewOldLogsMoveIntoTheLogFolder(t *testing.T) {
	data := t.TempDir()
	old := filepath.Join(filepath.Dir(data), "text-logs")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "core-2026-10-21.log"), []byte("panel"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "vpn-core.log"), []byte("core"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "temp", "tunserve.log"), []byte("server"), 0o644); err != nil {
		if mkerr := os.MkdirAll(filepath.Join(data, "temp"), 0o755); mkerr != nil {
			t.Fatal(mkerr)
		}
		if err := os.WriteFile(filepath.Join(data, "temp", "tunserve.log"), []byte("server"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	logs := t.TempDir()

	migrateOldLogs(logs, data)

	for _, name := range []string{"core-2026-10-21.log", "vpn-core.log", "tunserve.log"} {
		if _, err := os.Stat(filepath.Join(logs, name)); err != nil {
			t.Errorf("%s did not move into the log folder: %v", name, err)
		}
	}
	// And the old places are empty of them.
	if _, err := os.Stat(filepath.Join(data, "vpn-core.log")); err == nil {
		t.Error("the old core log is still where it was")
	}
}
