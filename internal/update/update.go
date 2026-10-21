// Package update checks GitHub releases for a newer version, and downloads one.
package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Asset is one downloadable file of a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Release is a trimmed GitHub release entry.
type Release struct {
	TagName     string  `json:"tag_name"`
	Name        string  `json:"name"`
	HTMLURL     string  `json:"html_url"`
	Body        string  `json:"body"`
	Prerelease  bool    `json:"prerelease"`
	PublishedAt string  `json:"published_at"`
	Assets      []Asset `json:"assets"`
}

// Latest returns the newest release of repo (owner/name) the reader may install:
// the latest by publication time, with prereleases left out unless includePre
// asks for them.
//
// It says nothing about whether that release is newer than what the reader is
// running — that question belongs to whoever is asking, and the two answers it
// can give (a version number, a build time) are not the same question. See
// Compare and PublishedAfter.
func Latest(repo string, includePre bool) (*Release, error) {
	if repo == "" {
		return nil, fmt.Errorf("no repository configured")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+repo+"/releases", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Eagle-Go")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API: HTTP %d", resp.StatusCode)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}
	if len(releases) == 0 {
		return nil, fmt.Errorf("no release found")
	}
	// The newest by publication time, not by the order GitHub listed them in: that
	// order is by creation, and a back-filled or re-published older tag must not
	// be announced as the newest thing on offer. The timestamps are RFC3339 in UTC,
	// so their order is their order as text.
	var best *Release
	for i := range releases {
		r := &releases[i]
		if r.Prerelease && !includePre {
			continue
		}
		if best == nil || r.PublishedAt > best.PublishedAt {
			best = r
		}
	}
	// Everything on offer is a prerelease and the reader does not want those.
	if best == nil {
		return nil, nil
	}
	return best, nil
}

// PublishedAfter reports whether the release was published after the given
// moment. An undated release — an empty or unparseable published_at — counts as
// newer: hiding an update on a missing field is the more expensive mistake of
// the two.
func PublishedAfter(rel Release, after time.Time) bool {
	if after.IsZero() || rel.PublishedAt == "" {
		return true
	}
	published, err := time.Parse(time.RFC3339, rel.PublishedAt)
	if err != nil {
		return true
	}
	return published.After(after)
}

// Compare reads two version strings and says which is newer: -1, 0 or 1.
//
// It reads "v1.2.3", "1.2.3", "1.2.3-beta.1" and "1.2.3+build" — the "v" prefix,
// a pre-release suffix and a build suffix are all optional — and compares the
// numbers first, so 1.10.0 is newer than 1.9.0.
//
// The second answer is false when either side cannot be read as a version. The
// caller then has no version to compare and has to fall back on something else
// (the build time), rather than being told a version is newer because a string
// it did not understand happened to sort first.
func Compare(a, b string) (int, bool) {
	va, oka := parseVersion(a)
	vb, okb := parseVersion(b)
	if !oka || !okb {
		return 0, false
	}
	for i := 0; i < 3; i++ {
		if va.num[i] != vb.num[i] {
			if va.num[i] < vb.num[i] {
				return -1, true
			}
			return 1, true
		}
	}
	// Same numbers: a pre-release is older than the release it leads to, so
	// 1.2.0-beta.1 loses to 1.2.0 and beats 1.2.0-alpha.
	switch {
	case va.pre == vb.pre:
		return 0, true
	case va.pre == "":
		return 1, true
	case vb.pre == "":
		return -1, true
	default:
		return comparePre(va.pre, vb.pre), true
	}
}

// comparePre orders two pre-release suffixes by the semver rules.
//
// The suffix is split on dots into identifiers and they are compared one by
// one. An identifier made only of digits is compared as a NUMBER, so beta.10 is
// newer than beta.9 — a plain string comparison gets that backwards, because
// '1' sorts before '9'. A numeric identifier ranks below a non-numeric one,
// and when one suffix runs out of identifiers the longer one is the newer one.
func comparePre(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if c := comparePreIdentifier(as[i], bs[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	}
	return 0
}

// comparePreIdentifier orders two pre-release identifiers.
func comparePreIdentifier(a, b string) int {
	an, aNum := strconv.Atoi(a)
	bn, bNum := strconv.Atoi(b)
	switch {
	case aNum == nil && bNum == nil:
		if an != bn {
			if an < bn {
				return -1
			}
			return 1
		}
		return 0
	case aNum == nil:
		// A numeric identifier always has lower precedence than a non-numeric one.
		return -1
	case bNum == nil:
		return 1
	}
	if a != b {
		if a < b {
			return -1
		}
		return 1
	}
	return 0
}

// version is a version string read into its parts.
type version struct {
	num [3]int
	pre string
}

// parseVersion reads one version string. It is deliberately strict: a string
// that is not a version (a commit hash, a word, an empty string) is not a
// version, and saying so is what lets the caller fall back on another answer.
func parseVersion(s string) (version, bool) {
	var v version
	s = strings.TrimSpace(s)
	if s == "" {
		return v, false
	}
	// The "v" the tags carry is a decoration, not part of the number.
	if s[0] == 'v' || s[0] == 'V' {
		s = s[1:]
	}
	// Build metadata is ignored: two builds of the same version differ by it and
	// are the same version as far as an update check is concerned.
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	// A pre-release suffix is kept: it is what makes 1.2.0-beta older than 1.2.0.
	if i := strings.IndexByte(s, '-'); i >= 0 {
		v.pre = s[i+1:]
		s = s[:i]
	}
	if s == "" {
		return v, false
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v.num[i] = n
	}
	return v, true
}

// WindowsAsset picks the release's Windows executable: a .exe that does not
// name another platform.
//
// A release carries several platforms' builds, and taking the first .exe in the
// list would hand a Linux build to a Windows machine the day the order changes.
// The name is what says which platform a build is for, so the rule is read
// from it: an .exe is a Windows build unless its name says otherwise. That also
// keeps working for the assets published before the names carried a platform —
// "Eagle-v1.0.1.exe" is a Windows build, and so is "Eagle-v1.0.2-windows.exe".
func WindowsAsset(rel *Release) (*Asset, error) {
	for i := range rel.Assets {
		name := strings.ToLower(rel.Assets[i].Name)
		if !strings.HasSuffix(name, ".exe") {
			continue
		}
		if otherPlatform(name) {
			continue
		}
		return &rel.Assets[i], nil
	}
	return nil, fmt.Errorf("no Windows build in %s", rel.TagName)
}

// otherPlatform reports whether an asset name says it is for something other
// than Windows. The words are the ones a cross-platform release names its
// builds by; a name that says none of them is a Windows build.
func otherPlatform(name string) bool {
	for _, p := range []string{"macos", "darwin", "osx", "linux", "bsd", "android", "unix"} {
		if strings.Contains(name, p) {
			return true
		}
	}
	return false
}

// Download writes url to path, and refuses the result unless it is what the
// release said it would be: wantSize bytes (0 skips the check) and a Windows
// executable.
//
// The checks are not ceremony. This file is about to be moved over the running
// build by the swap script, with no backup and no way back, and the one network
// shape that answers a GitHub download is a captive portal: a hotel or office
// proxy replies HTTP 200 with a login page, and the script renamed that page to
// Eagle.exe. The reader's app then refused to start and the old build was gone.
// app_rulesets.go has paid the same two checks for the same reason.
//
// The timeout is generous because these are whole applications — tens of
// megabytes over whatever link the reader has.
func Download(url, path string, wantSize int64) error {
	if url == "" {
		return fmt.Errorf("no download url")
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, resp.Body)
	if err != nil {
		// A half-written file is worse than none: it would be moved over the running
		// build by the installer script.
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	if wantSize > 0 && n != wantSize {
		_ = os.Remove(path)
		return fmt.Errorf("下载不完整：得到 %d 字节，应为 %d 字节", n, wantSize)
	}
	if err := checkExecutable(path); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// checkExecutable refuses anything that does not begin with the DOS/PE magic
// ("MZ"). A response body that is HTML, a JSON error or an empty file all fail
// here, before the name of the running build is put on them.
func checkExecutable(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var head [2]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return fmt.Errorf("下载的文件不是可执行文件（读取失败）")
	}
	if head[0] != 'M' || head[1] != 'Z' {
		return fmt.Errorf("下载的文件不是 Windows 程序（可能被网络登录页或代理替换），已放弃安装")
	}
	return nil
}
