// Command robocity-sim runs a SimCode Robot City Builder city controller
// (main.go) locally against your city's CURRENT state, using an in-process port
// of the server engine — no GitHub push, no Redis. It compiles the user's
// UNCHANGED main.go against a local, engine-backed client library via a temporary go.work
// that overrides the published github.com/oduvan/simcode-go.
//
// Usage:
//
//	robocity-sim run     [dir-or-main.go] [--ticks N] [--json] [--quiet] [--city <slug>] [--server <url>]
//	robocity-sim inspect [--state | --logs N | --errors [--release all|SHA]] [--city <slug>] [--server <url>]
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
	case "check":
		return checkCmd(args[1:])
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
	seed := fs.Int("seed", -1, "run this exact world seed instead of your city's")
	canonical := fs.Bool("canonical", false, "run the module's canonical map instead of your city's world (use this if you have no city yet)")
	fromLive := fs.Bool("from-live", false, "start from your city AS IT IS NOW — its saved world and saved store, continuing its own tick numbering — instead of a fresh world")
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON")
	quiet := fs.Bool("quiet", false, "suppress the per-tick feed; print only the summary")
	city := fs.String("city", "", "city slug whose world to run (default: auto-detected from this repo's git remote)")
	skipCheck := fs.Bool("skip-code-check", false, "do not ask the server whether a deploy would accept this code")
	server := fs.String("server", "https://simcode.lyabah.com", "server base URL (engine download + world lookup)")
	fs.Usage = usage
	if err := fs.Parse(args); err != nil {
		return 2
	}
	// Exactly one world source. Naming two is a contradiction, not a preference.
	named := 0
	if *seed >= 0 {
		named++
	}
	if *canonical {
		named++
	}
	if *fromLive {
		named++
	}
	if named > 1 {
		fmt.Fprintln(os.Stderr, "error: --seed, --canonical and --from-live name different worlds; pick one")
		return 2
	}

	return cmdRun(runOptions{
		target:    target,
		ticks:     *ticks,
		seed:      *seed,
		canonical: *canonical,
		fromLive:  *fromLive,
		json:      *jsonOut,
		quiet:     *quiet,
		city:      *city,
		server:    *server,
		skipCheck: *skipCheck,
	})
}

// checkCmd runs ONLY the acceptance check: would a deploy accept this repo?
// It asks the server (the same rule a push runs) and simulates nothing.
func checkCmd(args []string) int {
	var target string
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		target = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	server := fs.String("server", "https://simcode.lyabah.com", "server base URL")
	fs.Usage = usage
	if err := fs.Parse(args); err != nil {
		return 2
	}
	pkgDir, _, err := resolveProject(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}
	return checkCode(*server, pkgDir, false)
}

func usage() {
	fmt.Fprint(os.Stderr, `robocity-sim — local runner for the SimCode Robot City Builder game

Runs your unchanged main.go against the REAL game engine (the exact c-shared library
the server runs, downloaded + cached on first use), fresh from tick 0. Run it inside
your city's repo and it uses that city's world — seed AND per-city config (public, no
token). Needs CGO_ENABLED=1 + a C compiler (the engine is loaded via cgo/dlopen).

If your city's world cannot be obtained, the run STOPS. It never silently uses a
different world. To run another one, ask for it: --seed N or --canonical. Every run
prints which world it used, and --json carries it in a "world" block.

Before simulating, "run" asks the server whether a deploy would ACCEPT your code,
using the same rule a real push runs. "check" runs just that step.

Usage:
  robocity-sim run     [dir-or-main.go] [flags]   # run your code against the real engine
  robocity-sim check   [dir-or-main.go] [flags]   # would a deploy accept this code? (no simulation)
  robocity-sim inspect [flags]                    # print live city info (public REST, no token)

run flags:
  --ticks N            ticks to simulate (default 500)
  --seed S             run this exact world seed instead of your city's
  --canonical          run the module's canonical map (use this if you have no city yet)
  --from-live          resume your city's CURRENT state (saved world + saved store)
  --json               emit a JSON document ({world,seed,ticks,city,summary,errors,feed})
  --quiet              suppress the per-tick feed; print only the SUMMARY
  --city SLUG          city slug whose world to run (default: auto-detected from the git remote)
  --skip-code-check    do not ask the server whether a deploy would accept this code
  --server URL         server base URL for engine download + world lookup (default https://simcode.lyabah.com)

exit codes:
  0 clean   3 your controller panicked   4 a deploy would REJECT this code
  5 could not consult the acceptance rule   6 could not obtain the world you asked for

inspect flags (all public REST — no token, no MCP):
  --state         full current world state    --logs N          recent activity log lines
  --errors        unhandled exceptions        --release all|SHA  widen --errors (default: current release)
  --city SLUG     city slug (default: auto-detected)   --server URL   server base URL

Environment:
  SIMCODE_ENGINE_SO   path to a local engine .so (skips the download; for engine devs / CI)
  SIMCODE_SERVER      override the engine-download / lookup server

Examples:
  robocity-sim run                       # run ./ against the real engine (this repo's city world)
  robocity-sim run . --ticks 300
  robocity-sim run examples/starter --canonical --ticks 120
  robocity-sim run . --from-live         # run against your city exactly as it is right now
  robocity-sim check                     # would a deploy accept this code?
  robocity-sim inspect                   # this city's status
  robocity-sim inspect --state           # full world state (JSON)
  robocity-sim inspect --logs 100        # recent activity log
  robocity-sim inspect --errors          # unhandled exceptions since your last release
`)
}
