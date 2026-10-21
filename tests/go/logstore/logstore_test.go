package logstore_test

import (
	. "eagle/internal/logstore"

	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A line written after the store is closed is a line the reader will look for and
// not find.
//
// The shutdown path closes the store and then writes its last breadcrumb
// ("[shutdown] stage: done"), so that line is written into a queue whose writer
// had already gone: it sat there until the queue filled up, and was then dropped
// silently. The breadcrumb that says the shutdown finished is the one a reader
// looks for when a run stops halfway, and it was the one line that could never
// arrive.
func TestWriteAfterCloseReachesTheFile(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	s.Write("core", "before the close")
	s.Close()
	s.Write("core", "after the close")

	path := filepath.Join(dir, "core-"+time.Now().Format("2006-01-02")+".log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log file: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "before the close") {
		t.Errorf("the line written before the close is missing: %q", text)
	}
	if !strings.Contains(text, "after the close") {
		t.Errorf("the line written after the close never reached the file: %q", text)
	}
	// The order matters: the late line lands after the ones before it, not in
	// the middle of them.
	if strings.Index(text, "after the close") < strings.Index(text, "before the close") {
		t.Errorf("the late line landed before the earlier one: %q", text)
	}
}

// A closed store must not make a reader wait for a writer that is gone: the
// flush marker would never be answered, and the wait was two seconds of
// nothing.
func TestFlushAfterCloseReturns(t *testing.T) {
	s := New(t.TempDir())
	s.Close()

	start := time.Now()
	s.Flush()
	if d := time.Since(start); d > time.Second {
		t.Errorf("Flush after Close took %v, want it to return at once", d)
	}
}
