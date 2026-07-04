// Command robocity-sim runs a SimCode Robot City Builder city controller
// (main.go) locally against an in-process port of the server engine — no GitHub
// push, no Redis, no network. It compiles the user's UNCHANGED main.go against a
// local, engine-backed SDK via a temporary go.work that overrides the published
// github.com/lyabah/simcode-sdk-go.
//
// Usage:
//
//	robocity-sim run [dir-or-main.go] [--ticks N] [--seed S] [--json] [--quiet]
//	                 [--from-live --city <slug> --server <url>]
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/oduvan/simcode-robocity-go-tools/sdklocal/engine"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	switch args[0] {
	case "run":
		return runCmd(args[1:])
	case "-h", "--help", "help":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		usage()
		return 2
	}
}

func runCmd(args []string) int {
	// The optional positional target (dir or main.go), when present, is the first
	// arg; the flags follow it. This matches the documented form
	// `robocity-sim run [dir-or-main.go] [flags]` and avoids Go's flag package
	// stopping at a leading non-flag.
	var target string
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		target = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	ticks := fs.Int("ticks", 500, "ticks to simulate")
	seed := fs.Int64("seed", engine.CanonicalSeed, "world seed (default 7, the canonical map)")
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON")
	quiet := fs.Bool("quiet", false, "suppress the per-tick feed; print only the summary")
	fresh := fs.Bool("fresh", false, "ignore the live city; run a clean seed-0 world (a new city / deterministic baseline). Default tests from your city's CURRENT state.")
	fromLive := fs.Bool("from-live", false, "(deprecated; live is the default now) accepted for compatibility")
	city := fs.String("city", "", "city slug to test against (default: auto-detected from this repo's git remote)")
	server := fs.String("server", "https://robocity.lyabah.com", "MCP server base URL")
	fs.Usage = usage
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Was --seed supplied explicitly? (so a live seed isn't overridden)
	seedSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "seed" {
			seedSet = true
		}
	})

	return cmdRun(runOptions{
		target:   target,
		ticks:    *ticks,
		seed:     *seed,
		seedSet:  seedSet,
		json:     *jsonOut,
		quiet:    *quiet,
		fresh:    *fresh,
		fromLive: *fromLive,
		city:     *city,
		server:   *server,
	})
}

func usage() {
	fmt.Fprint(os.Stderr, `robocity-sim — local offline simulator for the SimCode Robot City Builder game

Usage:
  robocity-sim run [dir-or-main.go] [flags]

Flags:
  --ticks N       ticks to simulate (default 500)
  --seed S        world seed (default 7, the canonical shared map)
  --json          emit a JSON document ({seed,ticks,city,summary,feed}) instead of text
  --quiet         suppress the per-tick feed; print only the SUMMARY
  --from-live     seed the world from a live city's public snapshot (approximate)
  --city SLUG     city slug (required with --from-live) / label
  --server URL    MCP server base URL (default https://robocity.lyabah.com)

Examples:
  robocity-sim run                      # run ./main.go, 500 ticks, seed 7
  robocity-sim run ./main.go --ticks 300
  robocity-sim run . --json
  robocity-sim run --from-live --city my-city   # needs SIMCODE_TOKEN
`)
}
