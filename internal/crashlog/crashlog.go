// Package crashlog records panics to a log file so crashes can be diagnosed.
package crashlog

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
)

// dirMu guards dir, the folder the crash record goes to when the caller does not
// name one.
var (
	dirMu sync.Mutex
	dir   string
)

// SetDir names the folder the crash record goes to.
//
// The app calls it once it has decided where the logs live — which is not always
// the folder beside the executable, because that folder is not always writable —
// and every panic recorded afterwards lands there with the rest of the logs.
func SetDir(d string) {
	dirMu.Lock()
	dir = d
	dirMu.Unlock()
}

// currentDir is the folder SetDir named, or the one beside the executable.
func currentDir() string {
	dirMu.Lock()
	d := dir
	dirMu.Unlock()
	if d != "" {
		return d
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "log")
	}
	return ""
}

// Install logs a panic (if any) to <dir>/crash.log and exits.
// Use as: defer crashlog.Install(dir)
func Install(dir string) {
	if r := recover(); r != nil {
		write(dir, fmt.Sprintf("panic: %v\n\n%s", r, debug.Stack()))
		os.Exit(1)
	}
}

// Write appends a message to the crash log.
func Write(dir, msg string) { write(dir, msg) }

// Guard runs a goroutine body with a record kept for it. A panic in any goroutine
// but main takes the whole process down, and the deferred handler in main does not
// run for it — so every long-lived goroutine this app starts was one silent crash
// away from leaving no trace at all. The panic is written where main's own would
// have gone, and the process then exits exactly as it would have anyway: recovering
// without exiting would leave an app running with the goroutine that was its engine
// missing, which is worse than a crash with a log.
func Guard(name string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			write("", fmt.Sprintf("panic in goroutine %s: %v\n\n%s", name, r, debug.Stack()))
			os.Exit(1)
		}
	}()
	fn()
}

func write(dir, msg string) {
	candidates := make([]string, 0, 2)
	if dir != "" {
		candidates = append(candidates, dir)
	} else {
		// Never the working directory. Launched from a Run key or a shortcut the
		// working directory is %SystemRoot% or wherever the shell happened to be,
		// and the write then fails silently — a crash with no record of it, which is
		// the one thing this package exists to prevent. The log folder beside the
		// executable first: that is where the reader can find it, and where every
		// other log of this app is.
		if d := currentDir(); d != "" {
			candidates = append(candidates, d)
		}
	}
	// Last resort, and never skipped: wherever the app runs from, something is
	// usually writable, and a permission problem in one folder is not a reason to
	// lose the record altogether.
	candidates = append(candidates, os.TempDir())
	for _, d := range candidates {
		if writeTo(d, msg) == nil {
			return
		}
	}
}

// writeTo appends one record to <dir>/crash.log.
func writeTo(dir, msg string) error {
	if dir == "" {
		return os.ErrInvalid
	}
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(filepath.Join(dir, "crash.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "==== %s ====\n%s\n\n", time.Now().Format(time.RFC3339), msg)
	return err
}
