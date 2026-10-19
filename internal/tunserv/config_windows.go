//go:build windows

package tunserv

import (
	"encoding/json"
	"os"
	"path/filepath"

	"eagle/internal/util"
)

// resolveAppPath forwards to the shared resolver so a relative data_dir or
// core_path means exactly what it means to the app: relative to the
// executable.
func resolveAppPath(p string) string { return util.ResolveAppPath(p) }

// appPaths is what the server needs from the app config: the data folder (the
// whole file system under the temp contract) and the core executable the
// commands name implicitly. The shapes mirror app.json — read only, never
// written: this process acts on the app's decisions, never makes its own.
type appPaths struct {
	DataDir string `json:"data_dir"`
	Core    string `json:"core_path"`
}

// appConfigPath is the same file the app reads both paths from, so a settings
// change in the UI is the server's truth without any extra channel. There is
// once copy per user profile — appConfigDir mirrors app.go's appConfigDir,
// which is allowed to fall back on the user's folder when it cannot ask
// Windows where that is.
func appConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir, _ = os.Getwd()
	}
	return filepath.Join(dir, "eagle", "app.json")
}

// appPathsOf reads and resolves both paths. A missing file reads defaults:
// "config" beside the executable, and the sing-box core beside the data root,
// mirrored from the app's own defaults so first runs stay policy-neutral.
func appPathsOf() appPaths {
	var raw struct {
		DataDir string `json:"data_dir"`
		Core    string `json:"core_path"`
	}
	if data, err := os.ReadFile(appConfigPath()); err == nil {
		_ = json.Unmarshal(data, &raw)
	}
	if raw.DataDir == "" {
		raw.DataDir = "config"
	}
	if raw.Core == "" {
		raw.Core = filepath.Join("sing-box", "sing-box.exe")
	}
	return appPaths{
		DataDir: resolveAppPath(raw.DataDir),
		Core:    resolveAppPath(raw.Core),
	}
}
