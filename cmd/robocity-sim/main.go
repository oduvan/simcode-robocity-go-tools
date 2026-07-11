// Command robocity-sim runs a SimCode Robot City Builder city controller
// (main.go) locally against your city's CURRENT state, using an in-process port
// of the server engine — no GitHub push, no Redis. It compiles the user's
// UNCHANGED main.go against a local, engine-backed SDK via a temporary go.work
// that overrides the published github.com/lyabah/simcode-sdk-go.
//
// Usage:
//
//	robocity-sim run [dir-or-main.go] [--ticks N] [--json] [--quiet]
//	                 [--city <slug>] [--server <url>]     (needs SIMCODE_TOKEN)
package main

import (
	"flag"
	"fmt"
	"os"
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
	case "inspect":
		return inspectCmd(args[1:])
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
	seed := fs.Int("seed", -1, "world seed (default: your city's seed, else the canonical map, 7)")
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON")
	quiet := fs.Bool("quiet", false, "suppress the per-tick feed; print only the summary")
	city := fs.String("city", "", "city slug whose map seed to borrow (default: auto-detected from this repo's git remote)")
	server := fs.String("server", "https://robocity.lyabah.com", "server base URL (engine download + seed lookup)")
	fs.Usage = usage
	if err := fs.Parse(args); err != nil {
		return 2
	}

	return cmdRun(runOptions{
		target: target,
		ticks:  *ticks,
		seed:   *seed,
		json:   *jsonOut,
		quiet:  *quiet,
		city:   *city,
		server: *server,
	})
}

func usage() {
	fmt.Fprint(os.Stderr, `robocity-sim — local runner for the SimCode Robot City Builder game

Runs your unchanged main.go against the REAL game engine (the exact c-shared library
the server runs, downloaded + cached on first use), fresh from tick 0. Run it inside
your city's repo and it borrows that city's map seed (public, no token). Needs
CGO_ENABLED=1 + a C compiler (the engine is loaded via cgo/dlopen).

Usage:
  robocity-sim run     [dir-or-main.go] [flags]   # run your code against the real engine
  robocity-sim inspect [flags]                    # print live city info (like the MCP tools)

run flags:
  --ticks N       ticks to simulate (default 500)
  --seed S        world seed (default: your city's seed, else the canonical map, 7)
  --json          emit a JSON document ({seed,ticks,city,summary,errors,feed}) instead of text
  --quiet         suppress the per-tick feed; print only the SUMMARY
  --city SLUG     city slug whose map seed to borrow (default: auto-detected from the git remote)
  --server URL    server base URL for engine download + seed lookup (default https://robocity.lyabah.com)

inspect flags:
  --state         full current world state    --logs N   recent activity log lines
  --list          list your cities            --city SLUG / --server URL

Environment:
  SIMCODE_ENGINE_SO   path to a local engine .so (skips the download; for engine devs / CI)
  SIMCODE_SERVER      override the engine-download / lookup server

Examples:
  robocity-sim run                       # run ./ against the real engine (this repo's city seed)
  robocity-sim run . --ticks 300
  robocity-sim run examples/starter --seed 7 --ticks 120
  robocity-sim inspect                   # this city's status
  robocity-sim inspect --state           # full world state (JSON)
  robocity-sim inspect --logs 100        # recent activity log
`)
}
