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

// worldOfCity reads a city's world SEED and its per-city CONFIG from the PUBLIC
// snapshot (GET {server}/api/city/{slug}/snapshot). A city's live state is public
// (same data the shareable live page uses), so NO token is needed.
//
// Both come from one fetch on purpose: borrowing the seed WITHOUT the config gives
// you the city's map but not its world — a city created with starting_fleet 5 would
// run locally with the module default. The Python tool has fetched both since #50;
// this one now does too, so the two languages test the same world.
//
// An error here is FATAL to the run by design (#73 req 2): the caller must stop,
// never quietly run some other world.
func worldOfCity(server, slug string) (int64, map[string]any, error) {
	b, err := publicGet(server, "/api/city/"+slug+"/snapshot")
	if err != nil {
		if err == errNotFound {
			return 0, nil, fmt.Errorf("no city %q on %s (its snapshot is 404)", slug, server)
		}
		return 0, nil, fmt.Errorf("could not read city %q from %s: %w", slug, server, err)
	}
	var snap struct {
		World struct {
			Seed   *int64         `json:"seed"`
			Config map[string]any `json:"config"`
		} `json:"world"`
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		return 0, nil, fmt.Errorf("could not read city %q world from %s: %w", slug, server, err)
	}
	if snap.World.Seed == nil {
		return 0, nil, fmt.Errorf("city %q snapshot from %s carries no world seed "+
			"(the city may not have ticked yet)", slug, server)
	}
	return *snap.World.Seed, snap.World.Config, nil
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

// --------------------------------------------------------------------------
// the city's CURRENT state (#73 req 1)
// --------------------------------------------------------------------------

// citySave is GET /api/city/{slug}/save: the durable world checkpoint plus the
// city-wide store. They are persisted under separate keys and BOTH are needed —
// the world alone gives a city that looks right and behaves wrong.
type citySave struct {
	Slug   string          `json:"slug"`
	Saved  bool            `json:"saved"`
	Type   string          `json:"type"`
	Seed   int64           `json:"seed"`
	Config map[string]any  `json:"config"`
	Tick   int64           `json:"tick"`
	Seq    int64           `json:"seq"`
	Save   json.RawMessage `json:"save"`  // the whole envelope: feed it to the engine as-is
	Store  json.RawMessage `json:"store"` // always an object; {} when empty

	// No version is recorded with a save today, so EngineVersion is "" and
	// EngineVersionSource is "none". Reported, never invented — see run.go.
	EngineVersion       string `json:"engine_version"`
	EngineVersionSource string `json:"engine_version_source"`
	ServerEngineVersion string `json:"server_engine_version"`

	// Reason/Error are only set on the 404 body.
	Reason string `json:"reason"`
	Error  string `json:"error"`
}

// savedWorldOfCity reads a city's resumable save. Every failure is FATAL to the
// run: "this city has no save yet" is not an invitation to invent an empty world.
func savedWorldOfCity(server, slug string) (citySave, error) {
	url := strings.TrimRight(server, "/") + "/api/city/" + slug + "/save"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return citySave{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return citySave{}, fmt.Errorf("could not reach %s to read the saved world of city %q: %w", server, slug, err)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return citySave{}, fmt.Errorf("could not read the saved world of city %q: %w", slug, readErr)
	}

	if resp.StatusCode == http.StatusNotFound {
		// A JSON body with a machine-readable reason, so "nothing saved yet" can
		// never be mistaken for a valid empty world.
		var miss citySave
		_ = json.Unmarshal(body, &miss)
		switch miss.Reason {
		case "no_save":
			return citySave{}, fmt.Errorf("city %q has no saved world yet — it has not run long "+
				"enough to checkpoint one. Run without --from-live to start a fresh world", slug)
		case "unknown_city":
			return citySave{}, fmt.Errorf("no city %q on %s", slug, server)
		}
		return citySave{}, fmt.Errorf("no saved world for city %q on %s", slug, server)
	}
	if resp.StatusCode != http.StatusOK {
		return citySave{}, fmt.Errorf("could not read the saved world of city %q from %s: %s", slug, server, resp.Status)
	}

	var doc citySave
	if err := json.Unmarshal(body, &doc); err != nil {
		return citySave{}, fmt.Errorf("could not read the saved world of city %q from %s: %w", slug, server, err)
	}
	if len(doc.Save) == 0 || string(doc.Save) == "null" {
		return citySave{}, fmt.Errorf("the saved world of city %q from %s is not usable "+
			"(no save envelope in the response)", slug, server)
	}
	return doc, nil
}

// citySnapshotRaw fetches the city's display snapshot as raw JSON. It seeds the
// READ MODEL of a resumed run (see resolveWorld); the engine's own delta after a
// restore is incremental, so without it the handlers would read a nearly empty
// world while the engine held the full one.
func citySnapshotRaw(server, slug string) (json.RawMessage, int64, error) {
	b, err := publicGet(server, "/api/city/"+slug+"/snapshot")
	if err != nil {
		return nil, 0, err
	}
	var head struct {
		Tick int64 `json:"tick"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return nil, 0, err
	}
	return json.RawMessage(b), head.Tick, nil
}

// --------------------------------------------------------------------------
// the acceptance rule for user code (#73 req 3 / forum post 18)
// --------------------------------------------------------------------------
//
// Whether a repo is acceptable is decided in ONE place — the server's
// loader.ValidateSources, the same function a real deploy runs. This tool keeps
// NO copy of the allow-lists; a copied list is exactly how `__slots__` came to
// pass locally and be refused on deploy. It posts the sources and takes the
// server's verdict.

// errValidatorUnavailable marks "I could not find out" as distinct from "your
// code is not acceptable". The two must never look the same.
var errValidatorUnavailable = fmt.Errorf("acceptance rule unavailable")

// codeVerdict is the server's answer for one repo.
type codeVerdict struct {
	OK      bool   `json:"ok"`
	Files   int    `json:"files"`
	Error   string `json:"error"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// skipDirs are directories a repo never deploys; excluded from the upload.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, ".idea": true, ".vscode": true,
	"__pycache__": true, ".venv": true, "venv": true,
}

const maxUpload = 1 << 20 // matches the server's repo size cap

// collectSources reads a project tree as {relative/path: text} for validation,
// mirroring what a deploy sees (minus VCS/cache dirs).
//
// Approximation, deliberately on the safe side: the deploy validates a fresh
// CLONE (committed files only) while this reads the working tree. The skip list
// drops the untracked noise a clone would not contain; anything else uncommitted
// is sent, so an uncommitted file can only make this stricter than the deploy,
// never laxer. The verdict on the code itself is the server's, unchanged.
func collectSources(dir string) (map[string]string, error) {
	out := map[string]string{}
	var total int64
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		total += info.Size()
		if total > maxUpload {
			return filepath.SkipAll // past the server's cap; let it judge what we have
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil // unreadable file: it still counts, but carries no source
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// validateSources asks the server whether this code would be accepted on deploy.
func validateSources(server, language string, files map[string]string) (codeVerdict, error) {
	body, err := json.Marshal(map[string]any{"language": language, "files": files})
	if err != nil {
		return codeVerdict{}, err
	}
	url := strings.TrimRight(server, "/") + "/api/code/validate"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return codeVerdict{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return codeVerdict{}, fmt.Errorf("%w: could not reach %s: %v", errValidatorUnavailable, server, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return codeVerdict{}, fmt.Errorf("%w: %s has no /api/code/validate endpoint "+
			"(server too old to tell us what a deploy would accept)", errValidatorUnavailable, server)
	}
	if resp.StatusCode != http.StatusOK {
		return codeVerdict{}, fmt.Errorf("%w: %s returned %s", errValidatorUnavailable, url, resp.Status)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return codeVerdict{}, fmt.Errorf("%w: %v", errValidatorUnavailable, err)
	}
	var v codeVerdict
	if err := json.Unmarshal(raw, &v); err != nil {
		return codeVerdict{}, fmt.Errorf("%w: unreadable verdict from %s: %v", errValidatorUnavailable, url, err)
	}
	return v, nil
}

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
