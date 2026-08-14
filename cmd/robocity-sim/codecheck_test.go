package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// #73 req 3: the tool consults the SERVER's acceptance rule, never its own copy.
// A copied allow-list is exactly how `__slots__` came to pass locally and be
// refused on deploy.

func TestCollectSourcesGathersTheProjectAndSkipsVCS(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "main.go"), "package main\n")
	mustWrite(t, filepath.Join(dir, "lib", "plan.go"), "package lib\n")
	mustWrite(t, filepath.Join(dir, ".git", "config"), "junk\n")

	files, err := collectSources(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["main.go"]; !ok {
		t.Fatalf("main.go missing: %v", keys(files))
	}
	if _, ok := files["lib/plan.go"]; !ok {
		t.Fatalf("lib/plan.go missing: %v", keys(files))
	}
	if _, ok := files[".git/config"]; ok {
		t.Fatalf(".git must not be uploaded: %v", keys(files))
	}
}

func TestCheckCodeAcceptsAndRejects(t *testing.T) {
	var lastLang string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/code/validate" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Language string            `json:"language"`
			Files    map[string]string `json:"files"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		lastLang = req.Language
		if _, bad := req.Files["bad.go"]; bad {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": false, "error": "bad.go:1: nope"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "files": len(req.Files)})
	}))
	defer srv.Close()

	good := t.TempDir()
	mustWrite(t, filepath.Join(good, "main.go"), "package main\n")
	if rc := checkCode(srv.URL, good, true); rc != 0 {
		t.Fatalf("clean project rejected: %d", rc)
	}
	if lastLang != "go" {
		t.Fatalf("the Go tool must ask for the Go rules, asked %q", lastLang)
	}

	bad := t.TempDir()
	mustWrite(t, filepath.Join(bad, "main.go"), "package main\n")
	mustWrite(t, filepath.Join(bad, "bad.go"), "package main\n")
	if rc := checkCode(srv.URL, bad, true); rc != exitCodeRejected {
		t.Fatalf("rejected project should exit %d, got %d", exitCodeRejected, rc)
	}
}

// "I could not find out" must never look like "accepted".
func TestCheckCodeDistinguishesUnavailableFromRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil) // a server too old to have the endpoint
	}))
	defer srv.Close()

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "main.go"), "package main\n")

	if rc := checkCode(srv.URL, dir, true); rc != exitCheckUnknown {
		t.Fatalf("unavailable validator should exit %d, got %d", exitCheckUnknown, rc)
	}
	if _, err := validateSources(srv.URL, "go", map[string]string{"main.go": ""}); !errors.Is(err, errValidatorUnavailable) {
		t.Fatalf("want errValidatorUnavailable, got %v", err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
