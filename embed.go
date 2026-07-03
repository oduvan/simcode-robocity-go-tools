// Package robocitytools embeds the local, engine-backed SDK source so the
// robocity-sim CLI can materialize it at runtime — the same whether the tool was
// obtained via `go install ...@latest` (source lives in the module cache and is
// baked into the binary) or a `git clone`. The materialized copy is a standalone
// module github.com/lyabah/simcode-sdk-go that the user's unchanged main.go
// compiles against via a temporary go.work.
package robocitytools

import "embed"

// SDKFiles is the embedded source of ./sdklocal (the simcode package + its engine
// subpackage). robocity-sim writes it to a temp dir, rewrites the import path
// prefix to github.com/lyabah/simcode-sdk-go, and adds a generated go.mod.
//
//go:embed all:sdklocal
var SDKFiles embed.FS
