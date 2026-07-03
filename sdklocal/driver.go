// The local simulation driver: the tick loop that wires the read-model SDK to the
// in-process engine, plus the feed / SUMMARY / JSON output. Mirrors the server
// engine's step (game/core/engine): intents produced by a dispatch lag one tick
// before they are applied, exactly as in production.
package simcode

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/oduvan/simcode-robocity-go-tools/sdklocal/engine"
)

// feedLine is one rendered activity-feed entry (tick + text).
type feedLine struct {
	Tick int64  `json:"tick"`
	Line string `json:"line"`
}

// Run executes the local simulation: for each of `ticks` ticks it applies the
// previous tick's intents, advances the engine, publishes state, dispatches
// events to handlers, and collects the resulting intents for the next tick. It
// streams the per-tick feed (human mode) and prints a SUMMARY, or a JSON document
// with --json. It returns nil (the signature matches the published city.Run()).
func (c *City) Run() error {
	var (
		pending         []engine.Intent
		feed            []feedLine
		robotsDestroyed int
	)

	human := !c.quiet && !c.json

	for t := int64(1); t <= c.ticks; t++ {
		// 1. Apply intents accumulated from the previous tick's dispatch.
		var events []engine.Event
		for _, it := range pending {
			events = append(events, c.eng.Submit(it, t)...)
		}
		pending = nil

		// 2. Advance one tick.
		events = append(events, c.eng.Advance(t)...)

		// 3. Publish authoritative state (so handlers read the current tick).
		c.publishState(t)

		// 4. Dispatch each emitted event; collect intents for the next tick.
		var newIntents []engine.Intent
		for _, e := range events {
			if e.Event == engine.EventRobotDestroyed {
				robotsDestroyed++
			}
			sev := toSimEvent(c.id, e)
			for _, env := range c.dispatch(sev) {
				newIntents = append(newIntents, toEngineIntent(env))
			}
		}
		pending = newIntents

		// 5. Drain and render the activity feed for this tick.
		for _, f := range c.eng.DrainFeed() {
			fl := feedLine{Tick: f.Tick, Line: f.Line()}
			feed = append(feed, fl)
			if human {
				fmt.Println(fl.Line)
			}
		}
	}

	summary := c.eng.Summary(c.ticks)
	summary.RobotsDestroyed = robotsDestroyed

	if c.json {
		c.printJSON(summary, feed)
	} else {
		c.printErrors()
		c.printSummary(summary)
	}
	// Non-zero exit when the controller raised, so CI / an AI loop notices (the
	// user's main.go usually ignores Run()'s return, so we signal via the code).
	if len(c.errors) > 0 {
		os.Exit(3)
	}
	return nil
}

// printErrors surfaces handler panics (isolated during the run) to stderr, so a
// crashing controller is visible locally instead of silently swallowed.
func (c *City) printErrors() {
	if len(c.errors) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "⚠ %d handler error(s) — your controller panicked:\n", len(c.errors))
	for i, e := range c.errors {
		if i >= 5 {
			fmt.Fprintf(os.Stderr, "  … and %d more\n", len(c.errors)-5)
			break
		}
		where := fmt.Sprintf("on '%s'", e.Event)
		if e.Robot != "" {
			where += fmt.Sprintf(" (robot %s)", e.Robot)
		}
		fmt.Fprintf(os.Stderr, "  - %s: %s\n", where, e.Err)
	}
}

// publishState builds the state.* JSON the read model consumes and installs it as
// the current snapshot, exactly reproducing the server's KV publish + SDK MGET.
func (c *City) publishState(tick int64) {
	sj := c.eng.StateJSON(tick, tick)
	vals := make([]string, len(stateKeys))
	for i, k := range stateKeys {
		vals[i] = sj[k]
	}
	c.snap = decodeSnapshot(vals)
}

func toSimEvent(city string, e engine.Event) Event {
	var payload map[string]any
	if len(e.Payload) > 0 {
		_ = json.Unmarshal(e.Payload, &payload)
	}
	return Event{City: city, Type: TypeEvent, Event: e.Event, Robot: e.Robot, Tick: e.Tick, Payload: payload}
}

func toEngineIntent(env intentEnvelope) engine.Intent {
	cmds := make([]engine.Command, 0, len(env.Commands))
	for _, cmd := range env.Commands {
		cmds = append(cmds, engine.Command{Cmd: cmd.Cmd, Args: cmd.Args})
	}
	return engine.Intent{Robot: env.Robot, Commands: cmds, Logs: env.Logs}
}

// ---- output ----

func (c *City) printSummary(s engine.SummaryData) {
	bar := "================================================" // 48 '='
	fmt.Println("")
	fmt.Println(bar)
	fmt.Println("SUMMARY")
	fmt.Println(bar)
	fmt.Printf("  final tick        : %d\n", s.FinalTick)
	fmt.Printf("  robots            : %d\n", s.Robots)
	fmt.Printf("  robots destroyed  : %d\n", s.RobotsDestroyed)
	fmt.Printf("  buildings         : %d\n", s.Buildings)
	if len(s.BuildingsByType) > 0 {
		keys := make([]string, 0, len(s.BuildingsByType))
		for k := range s.BuildingsByType {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := ""
		for i, k := range keys {
			if i > 0 {
				parts += ", "
			}
			parts += fmt.Sprintf("%s=%d", k, s.BuildingsByType[k])
		}
		fmt.Printf("    by type         : %s\n", parts)
	}
	fmt.Printf("  ore   (mined/stored): %d / %d\n", s.OreMined, s.OreStored)
	fmt.Printf("  metal (mined/stored): %d / %d\n", s.MetalMined, s.MetalStored)
	fmt.Printf("  spots found       : %d\n", s.SpotsFound)
	fmt.Printf("  discovered cells  : %d\n", s.DiscoveredCells)
	if len(c.errors) > 0 {
		fmt.Printf("  handler errors    : %d  <-- your controller raised (see above)\n", len(c.errors))
	}
}

// jsonOut is the machine-readable document shape: {seed,ticks,city,summary,feed}.
type jsonOut struct {
	Seed    int64          `json:"seed"`
	Ticks   int64          `json:"ticks"`
	City    string         `json:"city"`
	Summary jsonSummary    `json:"summary"`
	Errors  []handlerError `json:"errors"`
	Feed    []feedLine     `json:"feed"`
}

type jsonResource struct {
	Mined  int `json:"mined"`
	Stored int `json:"stored"`
}

type jsonSummary struct {
	FinalTick       int64          `json:"final_tick"`
	Robots          int            `json:"robots"`
	RobotsDestroyed int            `json:"robots_destroyed"`
	Buildings       int            `json:"buildings"`
	BuildingsByType map[string]int `json:"buildings_by_type"`
	Ore             jsonResource   `json:"ore"`
	Metal           jsonResource   `json:"metal"`
	SpotsFound      int            `json:"spots_found"`
	DiscoveredCells int            `json:"discovered_cells"`
}

func (c *City) printJSON(s engine.SummaryData, feed []feedLine) {
	if feed == nil {
		feed = []feedLine{}
	}
	out := jsonOut{
		Seed:  c.seed,
		Ticks: c.ticks,
		City:  c.id,
		Summary: jsonSummary{
			FinalTick:       s.FinalTick,
			Robots:          s.Robots,
			RobotsDestroyed: s.RobotsDestroyed,
			Buildings:       s.Buildings,
			BuildingsByType: s.BuildingsByType,
			Ore:             jsonResource{Mined: s.OreMined, Stored: s.OreStored},
			Metal:           jsonResource{Mined: s.MetalMined, Stored: s.MetalStored},
			SpotsFound:      s.SpotsFound,
			DiscoveredCells: s.DiscoveredCells,
		},
		Errors: append([]handlerError{}, c.errors...),
		Feed:   feed,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Fprintln(os.Stdout, string(b))
}
