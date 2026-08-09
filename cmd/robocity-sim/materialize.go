package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	robocitytools "github.com/oduvan/simcode-robocity-go-tools"
)

// devModulePrefix is the import path the embedded ./localclient source uses in the
// dev tree. When materialized as a standalone module we rewrite it to the
// client library path so the user's `import "github.com/oduvan/simcode-go"`
// resolves and the internal engine import resolves under the new module root.
const (
	devModulePrefix  = "github.com/oduvan/simcode-robocity-go-tools/localclient"
	clientModulePath = "github.com/oduvan/simcode-go"
)

// materializeClient writes the embedded local client library to a fresh temp directory as a
// standalone, stdlib-only module named github.com/oduvan/simcode-go and
// returns its path. The caller removes it. It rewrites every import of the dev
// prefix to the client library path (so `.../localclient/engine` becomes
// `github.com/oduvan/simcode-go/engine`) and drops *_test.go files.
func materializeClient() (string, error) {
	dir, err := os.MkdirTemp("", "robocity-client-*")
	if err != nil {
		return "", err
	}

	err = fs.WalkDir(robocitytools.ClientFiles, "localclient", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(p, "_test.go") || strings.HasSuffix(p, "/go.mod") {
			return nil
		}
		rel := strings.TrimPrefix(p, "localclient/") // engine/foo.go or bar.go
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if mkErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkErr != nil {
			return mkErr
		}
		raw, rErr := robocitytools.ClientFiles.ReadFile(p)
		if rErr != nil {
			return rErr
		}
		if strings.HasSuffix(p, ".go") {
			raw = []byte(strings.ReplaceAll(string(raw), devModulePrefix, clientModulePath))
		}
		return os.WriteFile(dst, raw, 0o644)
	})
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}

	goMod := "module " + clientModulePath + "\n\ngo 1.23\n"
	if wErr := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); wErr != nil {
		_ = os.RemoveAll(dir)
		return "", wErr
	}
	return dir, nil
}
