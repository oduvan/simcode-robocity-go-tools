package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
)

// inspectCmd prints a city's live info as JSON, no simulation. Everything comes
// from the server's PUBLIC REST API — no token, no MCP: state/status from the
// snapshot, --logs from /logs, --errors from /exceptions. The city is
// auto-detected from this repo's git remote (or pass --city).
func inspectCmd(args []string) int {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	state := fs.Bool("state", false, "full current world state (public snapshot)")
	logs := fs.Int("logs", -1, "recent activity log lines to fetch, e.g. --logs 100")
	errorsFlag := fs.Bool("errors", false, "unhandled exceptions since your last release")
	release := fs.String("release", "", "with --errors: 'all' or a commit SHA to widen (default: current release)")
	city := fs.String("city", "", "city slug (default: auto-detected from this repo's git remote)")
	server := fs.String("server", "https://robocity.lyabah.com", "server base URL")
	fs.Usage = usage
	if err := fs.Parse(args); err != nil {
		return 2
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
	case *errorsFlag: // unhandled exceptions since last release → PUBLIC /exceptions
		return printPublicExceptions(*server, c, *release)
	case *logs >= 0: // recent logs → PUBLIC /logs
		n := *logs
		if n == 0 {
			n = 100
		}
		return printPublicLogs(*server, c, n)
	case *state: // full world state → PUBLIC snapshot
		return printPublicSnapshot(*server, c)
	default: // compact status derived from the PUBLIC snapshot
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

func printPublicLogs(server, slug string, limit int) int {
	b, err := publicGet(server, fmt.Sprintf("/api/city/%s/logs?limit=%d", slug, limit))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return printJSONBytes(b)
}

func printPublicExceptions(server, slug, release string) int {
	path := "/api/city/" + slug + "/exceptions"
	if release != "" {
		path += "?release=" + url.QueryEscape(release)
	}
	b, err := publicGet(server, path)
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
		Discovered    []json.RawMessage `json:"discovered"`
		Stats         json.RawMessage   `json:"stats"`
		HandlerErrors int               `json:"handler_errors"`
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
		"handler_errors": snap.HandlerErrors,
	}
	if len(snap.Stats) > 0 {
		out["stats"] = snap.Stats
	}
	// Health SIGNAL: a raised handler leaves a robot uncommanded, so a "frozen"
	// city is usually this.
	if snap.HandlerErrors > 0 {
		out["hint"] = fmt.Sprintf("%d unhandled exception(s) since your last release — run: robocity-sim inspect --errors", snap.HandlerErrors)
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
