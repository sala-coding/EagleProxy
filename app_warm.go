package main

// app_warm.go — 把 TUN 启动要读的文件提前读进系统文件缓存。
//
// 一次 TUN 启动里，核心自己的 `sing-box started (2.44s)` 大部分花在读它自己、读规则集
// 和读配置上；开机后第一次启动要把这些字节从盘上现读。这里做的不是给核心准备缓存，而是
// 让它的下一次读命中操作系统的页面缓存 —— 读的是同一批字节，读的时间挪到了机器空闲时。
//
// 只读不存：缓存归系统管，文件随后变了就是变了，核心照旧读它自己那份。

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// warmFileLimit is the size above which a file is left alone. The core itself is the
// largest thing read here and measured 76 MB on this machine (a full sing-box build),
// so the cap sits well above it; it exists for a rule set that arrives as a monolith,
// where the read would evict more than it warms.
const warmFileLimit = 256 << 20

// warmCoreFiles reads the core executable and every rule set the profile can reach.
func (a *App) warmCoreFiles() {
	start := time.Now()
	var files int
	var bytes int64
	warm := func(p string) {
		if n, ok := warmFile(p); ok {
			files++
			bytes += n
		}
	}
	if p := a.curCorePath(); p != "" {
		warm(p)
	} else if p := warmCoreInExecDir(); p != "" {
		warm(p)
	}
	if a.curSetting() != nil {
		dir := a.curSetting().RuleSetDir()
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if strings.EqualFold(filepath.Ext(path), ".srs") {
				warm(path)
			}
			return nil
		})
	}
	if files > 0 {
		a.logf(fmt.Sprintf("[warm] 已预热核心文件 %d 个 / %.1f MB（%v）", files, float64(bytes)/(1<<20), time.Since(start).Round(10*time.Millisecond)))
	}
}

// warmCoreInExecDir finds the core when the reader never picked one: the controller
// resolves it through the same two folders (see ListCoreFiles), so the newest
// sing-box*.exe in them is the process a start is about to run.
func warmCoreInExecDir() string {
	dir := execDir()
	if dir == "" {
		return ""
	}
	var best string
	var bestTime time.Time
	for _, d := range []string{dir, filepath.Join(dir, "sing-box")} {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := strings.ToLower(e.Name())
			if e.IsDir() || !strings.HasPrefix(name, "sing-box") || !strings.HasSuffix(name, ".exe") {
				continue
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			if fi.ModTime().After(bestTime) {
				best, bestTime = filepath.Join(d, e.Name()), fi.ModTime()
			}
		}
	}
	return best
}

// warmFile reads one file to the end and throws the bytes away, reporting how many
// it read and whether it read at all.
func warmFile(path string) (int64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || fi.IsDir() || fi.Size() > warmFileLimit {
		return 0, false
	}
	n, err := io.Copy(io.Discard, f)
	return n, err == nil
}
