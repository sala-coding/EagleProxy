package main

import (
	"fmt"
	"os"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ExportLogs writes the given text to a file the reader picks, and returns that path —
// or an empty string when the dialog was dismissed.
//
// The text is handed in by the window rather than assembled here: what a reader wants to
// save is what is on screen, which is one of four streams after a category and a search
// have been applied, and this side has no way to know which.
func (a *App) ExportLogs(text string) (string, error) {
	if a.curCtx() == nil {
		return "", fmt.Errorf("backend not initialised")
	}
	path, err := runtime.SaveFileDialog(a.curCtx(), runtime.SaveDialogOptions{
		Title:           "导出日志",
		DefaultFilename: "eagle-" + time.Now().Format("20060102-150405") + ".txt",
		Filters:         []runtime.FileFilter{{DisplayName: "文本文件 (*.txt)", Pattern: "*.txt"}},
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		// dismissed, which is not a failure
		return "", nil
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
