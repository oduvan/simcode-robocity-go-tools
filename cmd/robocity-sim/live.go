package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// seedForCity reads a city's world seed from its PUBLIC snapshot
// (GET {server}/api/city/{slug}/snapshot). A city's live state is public (same data
// the shareable live page uses), so NO token is needed. A fresh local run uses this
// seed so the local map matches the live city's map.
func seedForCity(server, slug string) (int64, error) {
	b, err := publicGet(server, "/api/city/"+slug+"/snapshot")
	if err != nil {
		return 0, err
	}
	var snap struct {
		World struct {
			Seed int64 `json:"seed"`
		} `json:"world"`
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		return 0, err
	}
	return snap.World.Seed, nil
}

// slugForRepo resolves a repo ("owner/name") to its city slug via the PUBLIC
// endpoint — no token. Returns "" if no city is linked to that repo.
func slugForRepo(server, repo string) (string, error) {
	b, err := publicGet(server, "/api/city-by-repo/"+strings.Trim(repo, "/"))
	if err != nil {
		if err == errNotFound {
			return "", nil
		}
		return "", err
	}
	var d struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return "", err
	}
	return d.Slug, nil
}

var errNotFound = fmt.Errorf("not found")

// publicGet fetches a public (no-auth) endpoint and returns the raw body.
func publicGet(server, path string) ([]byte, error) {
	url := strings.TrimRight(server, "/") + path
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// gitRepoSlug returns the `owner/repo` of the git remote in dir, or "".
func gitRepoSlug(dir string) string {
	if dir == "" {
		dir = "."
	}
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return parseRepoSlug(strings.TrimSpace(string(out)))
}

// parseRepoSlug turns a git remote URL into `owner/repo`:
// git@github.com:owner/repo.git | https://github.com/owner/repo(.git).
func parseRepoSlug(url string) string {
	url = strings.TrimSuffix(strings.TrimSpace(url), ".git")
	if url == "" {
		return ""
	}
	var path string
	switch {
	case strings.HasPrefix(url, "git@") && strings.Contains(url, ":"):
		path = url[strings.Index(url, ":")+1:]
	case strings.Contains(url, "://"):
		rest := url[strings.Index(url, "://")+3:]
		if i := strings.Index(rest, "/"); i >= 0 {
			path = rest[i+1:]
		} else {
			path = rest
		}
	default:
		path = url
	}
	var parts []string
	for _, p := range strings.Split(path, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], "/")
	}
	return ""
}
