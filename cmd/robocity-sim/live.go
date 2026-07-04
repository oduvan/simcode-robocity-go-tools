package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// fetchLiveSnapshot pulls a city's PUBLIC world snapshot from the MCP endpoint
// (POST {server}/mcp, JSON-RPC tools/call → get_world_state) and writes the
// world-state document to a file in workDir, returning its path. Best-effort and
// APPROXIMATE: the public snapshot is fog-limited and hides spot richness /
// in-flight command internals, so a --from-live run is a rough preview, not an
// exact continuation. Needs SIMCODE_TOKEN. Stdlib net/http only.
func fetchLiveSnapshot(server, city, workDir string) (string, error) {
	token := os.Getenv("SIMCODE_TOKEN")
	if token == "" {
		return "", fmt.Errorf("SIMCODE_TOKEN is not set (export a bearer token for the MCP server, then re-run --from-live)")
	}

	reqBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "get_world_state",
			"arguments": map[string]any{"city": city},
		},
	})

	url := server
	for len(url) > 0 && url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}
	url += "/mcp"

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("MCP server returned %s", resp.Status)
	}

	var rpc map[string]json.RawMessage
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&rpc); err != nil {
		return "", fmt.Errorf("decoding MCP response: %w", err)
	}

	worldState, err := extractWorldState(rpc)
	if err != nil {
		return "", err
	}

	path := filepath.Join(workDir, "live-snapshot.json")
	if err := os.WriteFile(path, worldState, 0o644); err != nil {
		return "", err
	}
	return path, nil
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

// detectCity asks the server (list_cities) for the city slug linked to repo, or "".
func detectCity(server, token, repo string) (string, error) {
	rpc, err := mcpCall(server, token, "list_cities", map[string]any{})
	if err != nil {
		return "", err
	}
	doc, err := contentJSON(rpc)
	if err != nil {
		return "", err
	}
	var d struct {
		Cities []struct {
			Slug string `json:"slug"`
			Repo string `json:"repo"`
		} `json:"cities"`
	}
	if err := json.Unmarshal(doc, &d); err != nil {
		return "", err
	}
	for _, c := range d.Cities {
		if strings.EqualFold(c.Repo, repo) {
			return c.Slug, nil
		}
	}
	return "", nil
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

// extractWorldState pulls the world-state JSON out of a JSON-RPC tools/call
// result. MCP returns content as a list of {type:"text", text:"...json..."}.
func extractWorldState(rpc map[string]json.RawMessage) ([]byte, error) {
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
		for _, block := range result.Content {
			if block.Type != "text" {
				continue
			}
			if ws, ok := unwrapWorld([]byte(block.Text)); ok {
				return ws, nil
			}
		}
	}
	// Some servers may return the world-state object directly under result.
	if ws, ok := unwrapWorld(raw); ok {
		return ws, nil
	}
	return nil, fmt.Errorf("could not parse world state from MCP response")
}

// unwrapWorld returns the inner world-state document (with top-level world/robots/
// buildings/tiles). get_world_state wraps it as {slug, type, deploy_status,
// state:{...}}, so the snapshot lives under "state"; older/bare shapes are also
// accepted. ok=false when the JSON isn't a world snapshot.
func unwrapWorld(b []byte) ([]byte, bool) {
	var doc map[string]json.RawMessage
	if json.Unmarshal(b, &doc) != nil {
		return nil, false
	}
	if st, ok := doc["state"]; ok && hasWorld(st) {
		return st, true
	}
	if hasWorld(b) {
		return b, true
	}
	return nil, false
}

func hasWorld(b []byte) bool {
	var doc map[string]json.RawMessage
	if json.Unmarshal(b, &doc) != nil {
		return false
	}
	_, hasWorld := doc["world"]
	_, hasRobots := doc["robots"]
	return hasWorld || hasRobots
}
