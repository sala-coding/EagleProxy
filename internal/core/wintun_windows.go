//go:build windows

package core

import (
	"os"
	"path/filepath"
)

// WintunReady reports whether the wintun driver DLL can be found where sing-box
// will look for it.
//
// sing-tun loads "wintun.dll" by name, so it comes from the process's own
// directory or the working directory — and the core is started with its working
// directory set to the config dir, not to the folder the executable lives in.
// The DLL is not shipped inside sing-box: it has to be dropped in by whoever
// assembles the package, and when it is missing the core fails with a bare
// "The specified module could not be found" that says nothing about which
// module, let alone where to put it. Checking here turns that into an
// instruction.
func WintunReady(coreExe, configDir string) bool {
	for _, dir := range []string{filepath.Dir(coreExe), configDir, filepath.Dir(filepath.Dir(coreExe))} {
		if dir == "" || dir == "." {
			continue
		}
		if st, err := os.Stat(filepath.Join(dir, "wintun.dll")); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}
