package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

// inspectCmd prints a city's live info (state/status/logs) or lists your cities —
// a thin client over the same MCP tools, no simulation. Output is JSON.
func inspectCmd(args []string) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	state := fs.Bool("state", false, "full current world state")
	logs := fs.Int("logs", -1, "recent activity log lines to fetch, e.g. --logs 100")
	list := fs.Bool("list", false, "list your cities (no city needed)")
	city := fs.String("city", "", "city slug (default: auto-detected from this repo's git remote)")
	server := fs.String("server", "https://robocity.lyabah.com", "MCP server base URL")
	fs.Usage = usage
	if err := fs.Parse(args); err != nil {
		return 2
	}

	token := os.Getenv("SIMCODE_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, `error: set SIMCODE_TOKEN first (dashboard → "Connect via MCP").`)
		return 2
	}

	if *list {
		return printMCP(*server, token, "list_cities", map[string]any{})
	}

	c := *city
	if c == "" {
		repo := gitRepoSlug(".")
		if repo == "" {
			fmt.Fprintln(os.Stderr, "error: run this inside your city's git repo, or pass --city <slug>.")
			return 2
		}
		slug, err := detectCity(*server, token, repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		if slug == "" {
			fmt.Fprintf(os.Stderr, "error: no city on %s is linked to %s.\n", *server, repo)
			return 2
		}
		c = slug
	}

	switch {
	case *state:
		return printMCP(*server, token, "get_world_state", map[string]any{"city": c})
	case *logs >= 0:
		n := *logs
		if n == 0 {
			n = 100
		}
		return printMCP(*server, token, "get_recent_logs", map[string]any{"city": c, "limit": n})
	default: // the quick status overview
		return printMCP(*server, token, "get_world_status", map[string]any{"city": c})
	}
}

// printMCP calls one MCP tool and pretty-prints its document as JSON.
func printMCP(server, token, name string, args map[string]any) int {
	rpc, err := mcpCall(server, token, name, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	doc, err := contentJSON(rpc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	var v any
	if json.Unmarshal(doc, &v) == nil {
		b, _ := json.MarshalIndent(v, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Println(string(doc))
	}
	return 0
}
