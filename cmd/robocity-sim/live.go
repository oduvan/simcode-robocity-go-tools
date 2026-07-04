package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// fetchLiveSnapshot pulls a city's CURRENT world snapshot from the PUBLIC endpoint
// (GET {server}/api/city/{slug}/snapshot) and writes it to a file in workDir,
// returning its path. A city's live state is public (same data the shareable live
// page uses), so NO token is needed. APPROXIMATE by construction (fog-limited,
// no in-flight command internals) — a preview, not an exact continuation.
func fetchLiveSnapshot(server, city, workDir string) (string, error) {
	snap, err := publicGet(server, "/api/city/"+city+"/snapshot")
	if err != nil {
		return "", err
	}
	path := filepath.Join(workDir, "live-snapshot.json")
	if err := os.WriteFile(path, snap, 0o644); err != nil {
		return "", err
	}
	return path, nil
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

// mcpCall POSTs a JSON-RPC tools/call to {server}/mcp and returns the raw result map.
func mcpCall(server, token, name string, args map[string]any) (map[string]json.RawMessage, error) {
	reqBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	url := strings.TrimRight(server, "/") + "/mcp"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MCP server returned %s", resp.Status)
	}
	var rpc map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&rpc); err != nil {
		return nil, err
	}
	return rpc, nil
}

// contentJSON pulls the first text content block's JSON out of a tools/call result.
func contentJSON(rpc map[string]json.RawMessage) ([]byte, error) {
	raw, ok := rpc["result"]
	if !ok {
		return nil, fmt.Errorf("MCP response had no result")
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &result); err == nil {
		for _, b := range result.Content {
			if b.Type == "text" {
				return []byte(b.Text), nil
			}
		}
	}
	return raw, nil
}
