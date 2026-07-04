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
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON")
	quiet := fs.Bool("quiet", false, "suppress the per-tick feed; print only the summary")
	city := fs.String("city", "", "city slug to test against (default: auto-detected from this repo's git remote)")
	server := fs.String("server", "https://robocity.lyabah.com", "MCP server base URL")
	fs.Usage = usage
	if err := fs.Parse(args); err != nil {
		return 2
	}

	return cmdRun(runOptions{
		target: target,
		ticks:  *ticks,
		json:   *jsonOut,
		quiet:  *quiet,
		city:   *city,
		server: *server,
	})
}

func usage() {
	fmt.Fprint(os.Stderr, `robocity-sim — local offline simulator for the SimCode Robot City Builder game

Tests your code against your city's CURRENT state (needs SIMCODE_TOKEN). Run it
inside your city's repo; the city is auto-detected from the git remote.

Usage:
  robocity-sim run [dir-or-main.go] [flags]

Flags:
  --ticks N       ticks to simulate (default 500)
  --json          emit a JSON document ({seed,ticks,city,summary,errors,feed}) instead of text
  --quiet         suppress the per-tick feed; print only the SUMMARY
  --city SLUG     city slug to test against (default: auto-detected from this repo's git remote)
  --server URL    MCP server base URL (default https://robocity.lyabah.com)

Examples:
  export SIMCODE_TOKEN=...               # dashboard → "Connect via MCP"
  robocity-sim run                       # test ./ against this repo's city, current state
  robocity-sim run . --ticks 300
  robocity-sim run . --city my-city --json
`)
}
