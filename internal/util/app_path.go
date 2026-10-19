package util

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolveAppPath turns a path as it was stored or typed into the absolute path
// the app runs on. An absolute path is itself; a relative one is taken against
// the folder the executable sits in — never against the process's working
// directory, which swing with however the app was launched — so "naive.exe"
// or "config" written beside the app keep meaning the same thing wherever the
// folder is unpacked or started from. This is the convention the data
// directory and the core path follow (app.go's resolveAppPath defers here),
// and every path the reader may type as a relative one goes through it.
func ResolveAppPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	base := ""
	if exe, err := os.Executable(); err == nil {
		base = filepath.Dir(exe)
	} else if wd, err := os.Getwd(); err == nil {
		base = wd
	}
	if base != "" {
		return filepath.Clean(filepath.Join(base, p))
	}
	return filepath.Clean(p)
}
