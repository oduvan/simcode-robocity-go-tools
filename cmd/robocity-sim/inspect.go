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

	// --list lists YOUR cities → inherently owner-scoped, needs the token.
	if *list {
		if token == "" {
			fmt.Fprintln(os.Stderr, "error: --list needs SIMCODE_TOKEN (it lists your cities).")
			return 2
		}
		return printMCP(*server, token, "list_cities", map[string]any{})
	}

	// Resolve the city — token-free via the public repo->slug lookup.
	c := *city
	if c == "" {
		repo := gitRepoSlug(".")
		if repo == "" {
			fmt.Fprintln(os.Stderr, "error: run this inside your city's git repo, or pass --city <slug>.")
			return 2
		}
		slug, err := slugForRepo(*server, repo)
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
	case *logs >= 0: // recent logs → authed MCP tool
		if token == "" {
			fmt.Fprintln(os.Stderr, "error: --logs needs SIMCODE_TOKEN.")
			return 2
		}
		n := *logs
		if n == 0 {
			n = 100
		}
		return printMCP(*server, token, "get_recent_logs", map[string]any{"city": c, "limit": n})
	case *state: // full world state → PUBLIC snapshot (no token)
		return printPublicSnapshot(*server, c)
	default: // compact status derived from the PUBLIC snapshot (no token)
		return printStatus(*server, c)
	}
}

func printPublicSnapshot(server, slug string) int {
	b, err := publicGet(server, "/api/city/"+slug+"/snapshot")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return printJSONBytes(b)
}

func printStatus(server, slug string) int {
	b, err := publicGet(server, "/api/city/"+slug+"/snapshot")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	var snap struct {
		Tick  int64 `json:"tick"`
		World struct {
			Seed int64 `json:"seed"`
		} `json:"world"`
		Robots    []json.RawMessage `json:"robots"`
		Buildings []struct {
			Type string `json:"type"`
		} `json:"buildings"`
		Discovered []json.RawMessage `json:"discovered"`
		Stats      json.RawMessage   `json:"stats"`
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	byType := map[string]int{}
	for _, bl := range snap.Buildings {
		byType[bl.Type]++
	}
	out := map[string]any{
		"city": slug, "tick": snap.Tick, "seed": snap.World.Seed,
		"robots": len(snap.Robots), "buildings": len(snap.Buildings),
		"buildings_by_type": byType, "discovered_cells": len(snap.Discovered),
	}
	if len(snap.Stats) > 0 {
		out["stats"] = snap.Stats
	}
	bb, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(bb))
	return 0
}

func printJSONBytes(b []byte) int {
	var v any
	if json.Unmarshal(b, &v) == nil {
		bb, _ := json.MarshalIndent(v, "", "  ")
		fmt.Println(string(bb))
	} else {
		fmt.Println(string(b))
	}
	return 0
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
