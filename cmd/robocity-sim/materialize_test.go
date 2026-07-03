package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMaterializeSDK checks the embedded SDK is written as a standalone module
// with the published module path, the engine subpackage present, the dev import
// prefix rewritten, and no *_test.go leaking in.
func TestMaterializeSDK(t *testing.T) {
	dir, err := materializeSDK()
	if err != nil {
		t.Fatalf("materializeSDK: %v", err)
	}
	defer os.RemoveAll(dir)

	// go.mod declares the published SDK module path.
	gm, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	if !strings.Contains(string(gm), "module "+sdkModulePath) {
		t.Fatalf("go.mod module path wrong:\n%s", gm)
	}

	// The root simcode package and the engine subpackage exist.
	if _, err := os.Stat(filepath.Join(dir, "sdk.go")); err != nil {
		t.Fatalf("root simcode source missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "engine", "module.go")); err != nil {
		t.Fatalf("engine subpackage missing: %v", err)
	}

	// The dev import prefix was rewritten to the published path; none remain.
	sdkGo, err := os.ReadFile(filepath.Join(dir, "sdk.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sdkGo), devModulePrefix) {
		t.Fatalf("dev import prefix %q not rewritten in sdk.go", devModulePrefix)
	}
	if !strings.Contains(string(sdkGo), sdkModulePath+"/engine") {
		t.Fatalf("engine import not rewritten to published path in sdk.go")
	}

	// No test files leaked into the materialized SDK.
	err = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(p, "_test.go") {
			t.Fatalf("test file leaked into materialized SDK: %s", p)
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
	if err := writeGoWork(wf, "/abs/user", "/abs/sdk"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(wf)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "/abs/user") || !strings.Contains(s, "/abs/sdk") {
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
