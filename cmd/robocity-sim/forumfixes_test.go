package main

import (
	"strings"
	"testing"

	"github.com/oduvan/simcode-robocity-go-tools/localclient/enginedl"
)

// #26 / #27 / #28 — the default server was the HUB. simgit.io answers every
// /api/* path with the SPA's index.html at HTTP 200, so the tool did not get an
// error, it got HTML and died parsing it as JSON. Reported three times.
func TestDefaultServerIsTheGameNotTheHub(t *testing.T) {
	if enginedl.DefaultServer != "https://robocity.simgit.io" {
		t.Fatalf("DefaultServer = %q, want the game host", enginedl.DefaultServer)
	}
	host := strings.TrimPrefix(enginedl.DefaultServer, "https://")
	if host == "simgit.io" {
		t.Fatal("the bare hub serves the SPA on /api/*; every call would parse HTML as JSON")
	}
	if !strings.HasSuffix(host, ".simgit.io") {
		t.Fatalf("host %q is not a simgit.io subdomain", host)
	}
}

// The CLI flags must not carry their own copy of the host: three of them did,
// which is how one of them could be missed.
func TestNoCommandHardcodesTheHub(t *testing.T) {
	for _, f := range []string{"main.go", "inspect.go", "run.go"} {
		b, err := readIfExists(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, `"https://simgit.io"`) {
				t.Errorf("%s hardcodes the hub: %s", f, strings.TrimSpace(line))
			}
		}
	}
}

// The call SITE has to use the helper. Testing discoveredCells alone would still
// pass with `len(snap.Discovered)` left in place, which is the bug as reported.
func TestInspectUsesTheCellCountNotLen(t *testing.T) {
	b, err := readIfExists("inspect.go")
	if err != nil {
		t.Fatalf("read inspect.go: %v", err)
	}
	src := string(b)
	if strings.Contains(src, `"discovered_cells": len(`) {
		t.Error("inspect reports len(runs) as discovered_cells — that is forum #30")
	}
	if !strings.Contains(src, `"discovered_cells": discoveredCells(`) {
		t.Error("inspect does not route discovered_cells through discoveredCells()")
	}
}

// #30 — `inspect` printed the number of RLE RUNS as `discovered_cells`. One
// city read 40 when it had discovered 1198.
func TestDiscoveredCellsCountsCellsNotRuns(t *testing.T) {
	for _, tc := range []struct {
		name string
		runs [][]int
		want int
	}{
		{"one run, inclusive ends", [][]int{{0, -19, 18}}, 38},
		{"a single cell", [][]int{{0, 0, 0}}, 1},
		{"two full rows", [][]int{{0, 0, 9}, {1, 0, 9}}, 20},
		{"nothing", nil, 0},
		{"a malformed run is ignored", [][]int{{0, 0, 4}, {7}}, 5},
	} {
		if got := discoveredCells(tc.runs); got != tc.want {
			t.Errorf("%s: discoveredCells = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// #31 — saves now record the build that wrote them, so the engine check is a
// real verdict instead of two version strings printed side by side.
func TestEngineCheckIsAVerdict(t *testing.T) {
	join := func(w resolvedWorld) string { return strings.Join(resumeCaveats(w), "\n") }

	ok := join(resolvedWorld{engineVersionSource: "save", engineVersion: "abc1234", serverEngineVersion: "abc1234"})
	if !strings.Contains(ok, "engine check: OK") || strings.Contains(ok, "NOT POSSIBLE") {
		t.Errorf("matching engines did not read as OK:\n%s", ok)
	}

	bad := join(resolvedWorld{engineVersionSource: "save", engineVersion: "abc1234", serverEngineVersion: "def5678"})
	if !strings.Contains(bad, "MISMATCH") {
		t.Errorf("a mismatch was not called one:\n%s", bad)
	}
	// The consequence has to stay on screen: this is the silent-corruption case.
	if !strings.Contains(bad, "partly-zeroed world") {
		t.Errorf("a mismatch did not say what it costs:\n%s", bad)
	}

	// A save from before the stamp: honest, not a guess.
	old := join(resolvedWorld{engineVersionSource: "none", serverEngineVersion: "abc1234"})
	if !strings.Contains(old, "NOT POSSIBLE") {
		t.Errorf("an unstamped save did not say it cannot be checked:\n%s", old)
	}
}
