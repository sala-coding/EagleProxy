// Package update checks GitHub releases for a newer version, and downloads one.
package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

// Check returns the newest release of repo (owner/name) that is newer than the build
// named by built, or nil when there is nothing newer — an answer the caller reports
// as "already up to date" rather than as a failure.
//
// built is the moment the running executable was written: the only version stamp
// this app carries (see app.buildTime — the About card's value is a commit hash,
// which no release tag can be ordered against). Without the comparison, "there is an
// update" was answered by whatever entry GitHub listed first, which is newest only by
// creation id: a back-filled or re-published older tag could be announced, and
// installed, as an upgrade.
func Check(repo string, includePre bool, built time.Time) (*Release, error) {
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
	for i := range releases {
		if releases[i].Prerelease && !includePre {
			continue
		}
		if !publishedAfter(releases[i], built) {
			continue
		}
		return &releases[i], nil
	}
	// Everything on offer is what this build already is.
	return nil, nil
}

// publishedAfter reports whether the release is newer than the build. An undated
// release — an empty or unparseable published_at — counts as newer: hiding an update
// on a missing field is the more expensive mistake of the two.
func publishedAfter(rel Release, built time.Time) bool {
	if built.IsZero() || rel.PublishedAt == "" {
		return true
	}
	published, err := time.Parse(time.RFC3339, rel.PublishedAt)
	if err != nil {
		return true
	}
	return published.After(built)
}

// WindowsAsset picks the release's Windows executable: the first asset whose name says
// windows and ends in .exe. A release carries several platforms' builds, and taking the
// first .exe in the list would hand a Linux build to a Windows machine the day the
// order changes.
func WindowsAsset(rel *Release) (*Asset, error) {
	for i := range rel.Assets {
		name := strings.ToLower(rel.Assets[i].Name)
		if strings.Contains(name, "windows") && strings.HasSuffix(name, ".exe") {
			return &rel.Assets[i], nil
		}
	}
	return nil, fmt.Errorf("no Windows build in %s", rel.TagName)
}

// Download writes url to path. The timeout is generous because these are whole
// applications — tens of megabytes over whatever link the reader has.
func Download(url, path string) error {
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
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		// A half-written file is worse than none: it would be moved over the running
		// build by the installer script.
		_ = os.Remove(path)
		return err
	}
	return f.Sync()
}
