package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	robocitytools "github.com/oduvan/simcode-robocity-go-tools"
)

// devModulePrefix is the import path the embedded ./sdklocal source uses in the
// dev tree. When materialized as a standalone module we rewrite it to the
// published SDK path so the user's `import "github.com/lyabah/simcode-sdk-go"`
// resolves and the internal engine import resolves under the new module root.
const (
	devModulePrefix = "github.com/oduvan/simcode-robocity-go-tools/sdklocal"
	sdkModulePath   = "github.com/lyabah/simcode-sdk-go"
)

// materializeSDK writes the embedded local SDK to a fresh temp directory as a
// standalone, stdlib-only module named github.com/lyabah/simcode-sdk-go and
// returns its path. The caller removes it. It rewrites every import of the dev
// prefix to the published SDK path (so `.../sdklocal/engine` becomes
// `github.com/lyabah/simcode-sdk-go/engine`) and drops *_test.go files.
func materializeSDK() (string, error) {
	dir, err := os.MkdirTemp("", "robocity-sdk-*")
	if err != nil {
		return "", err
	}

	err = fs.WalkDir(robocitytools.SDKFiles, "sdklocal", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(p, "_test.go") || strings.HasSuffix(p, "/go.mod") {
			return nil
		}
		rel := strings.TrimPrefix(p, "sdklocal/") // engine/foo.go or bar.go
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if mkErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkErr != nil {
			return mkErr
		}
		raw, rErr := robocitytools.SDKFiles.ReadFile(p)
		if rErr != nil {
			return rErr
		}
		if strings.HasSuffix(p, ".go") {
			raw = []byte(strings.ReplaceAll(string(raw), devModulePrefix, sdkModulePath))
		}
		return os.WriteFile(dst, raw, 0o644)
	})
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}

	goMod := "module " + sdkModulePath + "\n\ngo 1.23\n"
	if wErr := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); wErr != nil {
		_ = os.RemoveAll(dir)
		return "", wErr
	}
	return dir, nil
}
