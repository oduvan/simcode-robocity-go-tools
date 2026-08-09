package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMaterializeClient checks the embedded client library is written as a standalone module
// with the published module path, the engine subpackage present, the dev import
// prefix rewritten, and no *_test.go leaking in.
func TestMaterializeClient(t *testing.T) {
	dir, err := materializeClient()
	if err != nil {
		t.Fatalf("materializeClient: %v", err)
	}
	defer os.RemoveAll(dir)

	// go.mod declares the client library module path.
	gm, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	if !strings.Contains(string(gm), "module "+clientModulePath) {
		t.Fatalf("go.mod module path wrong:\n%s", gm)
	}

	// The root simcode package and the engine subpackage exist.
	if _, err := os.Stat(filepath.Join(dir, "client.go")); err != nil {
		t.Fatalf("root simcode source missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "engine", "engine.go")); err != nil {
		t.Fatalf("engine subpackage missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "enginedl", "enginedl.go")); err != nil {
		t.Fatalf("enginedl subpackage missing: %v", err)
	}

	// The dev import prefix was rewritten to the published path; none remain.
	clientGo, err := os.ReadFile(filepath.Join(dir, "client.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(clientGo), devModulePrefix) {
		t.Fatalf("dev import prefix %q not rewritten in client.go", devModulePrefix)
	}
	if !strings.Contains(string(clientGo), clientModulePath+"/engine") {
		t.Fatalf("engine import not rewritten to published path in client.go")
	}

	// No test files leaked into the materialized client library.
	err = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(p, "_test.go") {
			t.Fatalf("test file leaked into materialized client library: %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestWriteGoWork checks the generated workspace references both modules with
// absolute paths and no module-level replace (the override is by `use`).
func TestWriteGoWork(t *testing.T) {
	tmp := t.TempDir()
	wf := filepath.Join(tmp, "go.work")
	if err := writeGoWork(wf, "/abs/user", "/abs/client"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(wf)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "/abs/user") || !strings.Contains(s, "/abs/client") {
		t.Fatalf("go.work missing use directives:\n%s", s)
	}
	if strings.Contains(s, "replace") {
		t.Fatalf("go.work unexpectedly uses replace:\n%s", s)
	}
}

// TestResolveProjectFindsModuleRoot checks a main.go in a nested dir resolves to
// the enclosing module root.
func TestResolveProjectFindsModuleRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "cmd", "city")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mainGo := filepath.Join(sub, "main.go")
	if err := os.WriteFile(mainGo, []byte("package main\nfunc main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgDir, modRoot, err := resolveProject(mainGo)
	if err != nil {
		t.Fatal(err)
	}
	if pkgDir != sub {
		t.Fatalf("pkgDir = %s, want %s", pkgDir, sub)
	}
	if modRoot != root {
		t.Fatalf("modRoot = %s, want %s", modRoot, root)
	}
}
