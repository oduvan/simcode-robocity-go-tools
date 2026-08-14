// The local simulation driver: it drives the REAL game engine (a downloaded
// c-shared library, loaded over cgo — see the engine subpackage) one tick at a time
// and mirrors the per-tick delta into a world the read-model client library reads. This is the
// Go port of the Python tool's simcode/_local.py: same design as the browser —
//
//   - the engine returns a per-tick delta (`changes`); the first is the full starting
//     world, later ones are incremental;
//   - we keep a WorldMirror as maps keyed by id / "x,y", updated by applying each delta
//     field-wise (the same merge the browser reducer does);
//   - each tick we project the mirror into the client library's snapshot read model, dispatch the
//     tick's events through the UNCHANGED client library dispatch, and hand the produced intent
//     envelopes back as next tick's commands (intents lag one tick, like production).
//
// Only the transport differs; dispatch, the read model, the handles, and the
// command-accumulation path are all the untouched client library code.
package simcode

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/oduvan/simcode-robocity-go-tools/localclient/engine"
	"github.com/oduvan/simcode-robocity-go-tools/localclient/enginedl"
)

// engineModule selects which game module's engine to download (robot-city here).
const engineModule = "robot-city"

// feedLine is one rendered activity-feed entry (tick + text).
type feedLine struct {
	Tick int64  `json:"tick"`
	Line string `json:"line"`
}

// summaryData is the end-of-run scorecard, computed from the mirror (+ its running
// stats). It replaces the old in-process engine's SummaryData.
type summaryData struct {
	FinalTick int64
	Robots    int
	// #73 / forum post 23: end-of-life is NOT failure, so it is NOT the same
	// figure. RobotsExpired = flew past its lifespan (inevitable — build
	// replacements). RobotsDestroyed = battery hit 0 mid-flight (avoidable — the
	// controller mis-budgeted energy). Only the second means something is wrong.
	RobotsExpired      int
	RobotsDestroyed    int
	RobotsUnattributed int
	Buildings          int
	// The engine's OWN counts, from the delta's stats. They are authoritative;
	// the Robots/Buildings above are what the read model (and so the handlers)
	// could see. On a resumed run those two can disagree — see readModelDrift.
	EngineRobots    int
	EngineBuildings int
	BuildingsByType map[string]int
	OreMined        int
	OreStored       int
	MetalMined      int
	MetalStored     int
	SpotsFound      int
	DiscoveredCells int
	BaseLevel       int
}

// ---------------------------------------------------------------------------
// wire shapes (engine c-ABI: request in / response out)
// ---------------------------------------------------------------------------

type tickConfig struct {
	City string `json:"city"`
	Seed int64  `json:"seed"`
	// Type selects the game module. A resumed city states its own type, so a save
	// is never restored by the wrong module's rules.
	Type string `json:"type,omitempty"`
	// Config is the PER-CITY options blob (resource_intensity / starting_fleet /
	// seed …). Borrowing a city's seed without its config gave you that city's MAP
	// but not its WORLD — a city created with starting_fleet 5 ran locally with the
	// module default. The Python tool has carried it since #50; this one now does
	// too, so the two languages test the same world.
	Config map[string]any `json:"config,omitempty"`
}

// End-of-life events the runner always asks the engine for, even when the
// controller subscribes to neither, so the summary can report end-of-life and
// energy-death as separate figures.
const (
	lifecycleExpired   = EventRobotExpired
	lifecycleDestroyed = EventRobotDestroyed
)

// tickRequest is the EngineTick request. Map embeds the previous call's new_map
// (nil ⇒ marshals as JSON null ⇒ the engine's first call, generating the world).
type tickRequest struct {
	Config        tickConfig       `json:"config"`
	Subscriptions []string         `json:"subscriptions"`
	Map           json.RawMessage  `json:"map"`
	Commands      []intentEnvelope `json:"commands"`
}

// wireEvent is one event envelope the engine returns.
type wireEvent struct {
	Event   string          `json:"event"`
	Robot   string          `json:"robot"`
	Tick    int64           `json:"tick"`
	Payload json.RawMessage `json:"payload"`
}

// tickResponse is the EngineTick response.
type tickResponse struct {
	Events  []wireEvent     `json:"events"`
	Changes json.RawMessage `json:"changes"`
	NewMap  json.RawMessage `json:"new_map"`
	Tick    int64           `json:"tick"`
}

// Run drives the real engine for `ticks` ticks. Each tick it calls the engine with
// the current subscriptions + previous tick's intents, applies the returned delta to
// the mirror, projects the mirror into the read model, dispatches the tick's events,
// and collects the resulting intents for the next tick. It streams the per-tick feed
// (human mode) and prints a SUMMARY, or a JSON document with --json.
func (c *City) Run() error {
	// Resolve + load the real engine (download/cache, or $SIMCODE_ENGINE_SO).
	so, err := enginedl.EnsureEngine(engineModule)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not resolve the game engine: %v\n", err)
		os.Exit(2)
	}
	if err := engine.Load(so); err != nil {
		fmt.Fprintf(os.Stderr, "error: could not load the game engine: %v\n", err)
		os.Exit(2)
	}

	human := !c.quiet && !c.json

	mirror := newWorldMirror(c.id, c.seed)
	var (
		feed     []feedLine
		engMap   json.RawMessage  // opaque new_map; nil on the first call
		commands []intentEnvelope // intents to submit next tick
	)

	// Resuming a running city (#73 req 1): the saved world IS the engine's map
	// state, so the very first call restores instead of generating.
	if c.resumed {
		engMap = c.mapState
		// Saved values survive a deploy; in-memory values do not. Seed the store
		// and leave memory empty — exactly what a real push produces.
		for k, v := range c.initialStore {
			c.storeState[k] = v
		}
		// Seed the read model: a restored engine reports only the ticks it runs,
		// so without this the handlers would read a nearly empty world while the
		// engine held the full one.
		mirror.apply(c.prime)
	}

	for t := int64(0); t < c.ticks; t++ {
		// 1. Call the engine: current subscriptions, prev map, prev tick's intents.
		subscribed := c.subscribedEvents()
		reqBytes, _ := json.Marshal(tickRequest{
			Config:        tickConfig{City: c.id, Seed: c.seed, Type: c.moduleType, Config: c.cityConfig},
			Subscriptions: withLifecycleEvents(subscribed),
			Map:           engMap,
			Commands:      commands,
		})
		respBytes, err := engine.Tick(reqBytes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: engine tick failed: %v\n", err)
			os.Exit(2)
		}
		var resp tickResponse
		if err := json.Unmarshal(respBytes, &resp); err != nil {
			fmt.Fprintf(os.Stderr, "error: engine returned unreadable JSON: %v\n", err)
			os.Exit(2)
		}

		// 2. Apply the delta and remember the map for the next call.
		mirror.apply(resp.Changes)
		engMap = resp.NewMap

		// 3. Project the mirror into the read model (handlers read the current tick).
		c.publishState(mirror)

		// 4. Dispatch each emitted event; collect intents for the next tick.
		var newIntents []intentEnvelope
		for _, we := range resp.Events {
			switch we.Event {
			case lifecycleExpired:
				mirror.expired++
			case lifecycleDestroyed:
				mirror.destroyed++
			}
			sev := toSimEvent(c.id, we)
			for _, env := range c.dispatch(sev) {
				newIntents = append(newIntents, env)
				// The intent's memory/store writes persist across ticks the way the
				// SERVER persists them: as JSON. Round-tripping normalises Go ints to
				// float64, matching what a handler reads back from state on a later
				// tick (see the divergence note on applyIntentState).
				c.applyIntentState(env)
			}
		}
		commands = newIntents

		// 5. Render this tick's activity feed (game events + user r.Log lines).
		for _, fl := range renderFeed(resp.Changes) {
			feed = append(feed, fl)
			if human {
				fmt.Println(fl.Line)
			}
		}
	}

	summary := mirror.summary()

	if c.json {
		c.printJSON(summary, feed)
	} else {
		c.printErrors()
		c.printSummary(summary)
	}
	// Non-zero exit when the controller raised, so CI / an AI loop notices (the user's
	// main.go usually ignores Run()'s return, so we signal via the code).
	if len(c.errors) > 0 {
		os.Exit(3)
	}
	return nil
}

// withLifecycleEvents adds the two end-of-life events to what the controller
// asked for. Dispatch is unaffected — an event with no handler is a no-op — but
// the runner can now count expiry and energy-death separately regardless of what
// the controller subscribed to.
func withLifecycleEvents(subs []string) []string {
	out := append([]string(nil), subs...)
	for _, want := range []string{lifecycleExpired, lifecycleDestroyed} {
		found := false
		for _, s := range out {
			if s == want {
				found = true
				break
			}
		}
		if !found {
			out = append(out, want)
		}
	}
	sort.Strings(out)
	return out
}

// subscribedEvents is the sorted set of event names the controller subscribed to;
// the engine filters returned events to exactly these (like the live dispatch).
func (c *City) subscribedEvents() []string {
	out := make([]string, 0, len(c.handlers))
	for ev := range c.handlers {
		out = append(out, ev)
	}
	sort.Strings(out)
	return out
}

// applyIntentState persists an intent's store + per-robot memory writes into the
// city's in-process state, JSON-normalised.
//
// DIVERGENCE FROM THE PYTHON TOOL: Python keeps memory/store as live in-process
// dicts (a handler mutates r.memory in place), so an int stays an int. The Go client's
// generated starter reads memory with a typed assertion — r.Memory()["hop"].(float64)
// — because on the SERVER memory round-trips through JSON (Redis KV), where every
// number is a float64. To reproduce that server behaviour locally we round-trip the
// writes through JSON here; without it the assertion fails, the counter never
// advances, and exploration (hence the discovered-cell count) would diverge.
func (c *City) applyIntentState(env intentEnvelope) {
	if len(env.Store) > 0 {
		if norm, ok := jsonRoundTrip(env.Store).(map[string]any); ok {
			for k, v := range norm {
				c.storeState[k] = v
			}
		}
	}
	if len(env.Memory) > 0 {
		if norm, ok := jsonRoundTrip(env.Memory).(map[string]any); ok {
			c.memoryState[env.Robot] = norm
		}
	}
}

// jsonRoundTrip marshals then unmarshals v, normalising Go scalar types to what JSON
// decoding yields (numbers → float64), so cross-tick reads see server-shaped values.
func jsonRoundTrip(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}

// publishState projects the mirror into the read-model snapshot the handles read.
// It re-encodes the mirror into the same state.* JSON the server writes to KV and
// runs it through the unchanged decodeSnapshot — so a handle reflects the current
// tick exactly as it would in production (option (b): reuse the decoder verbatim).
func (c *City) publishState(m *worldMirror) {
	vals := make([]string, len(stateKeys))
	vals[0] = m.metaJSON()
	vals[1] = m.worldJSON()
	vals[2] = m.robotsJSON()
	vals[3] = m.buildingsJSON()
	vals[4] = m.spotsJSON()
	vals[5] = m.discoveredJSON()
	c.snap = decodeSnapshot(vals)
}

// toSimEvent converts a wire event envelope into the client library Event handlers receive.
func toSimEvent(city string, we wireEvent) Event {
	var payload map[string]any
	if len(we.Payload) > 0 {
		_ = json.Unmarshal(we.Payload, &payload)
	}
	return Event{City: city, Type: TypeEvent, Event: we.Event, Robot: we.Robot, Tick: we.Tick, Payload: payload}
}

// ---------------------------------------------------------------------------
// activity feed
// ---------------------------------------------------------------------------

// renderFeed turns the delta's `events` (the display feed, incl. user r.Log lines)
// into rendered feed lines.
func renderFeed(changes json.RawMessage) []feedLine {
	if len(changes) == 0 {
		return nil
	}
	var d struct {
		Tick   int64 `json:"tick"`
		Events []struct {
			Kind     string `json:"kind"`
			Robot    string `json:"robot"`
			Resource string `json:"resource"`
			Amount   int    `json:"amount"`
			Text     string `json:"text"`
			Tick     int64  `json:"tick"`
		} `json:"events"`
	}
	if json.Unmarshal(changes, &d) != nil {
		return nil
	}
	out := make([]feedLine, 0, len(d.Events))
	for _, e := range d.Events {
		tick := e.Tick
		if tick == 0 {
			tick = d.Tick
		}
		line := e.Text
		if line == "" {
			line = e.Kind
			if e.Resource != "" {
				line += fmt.Sprintf(" %s", e.Resource)
			}
			if e.Amount != 0 {
				line += fmt.Sprintf(" %d", e.Amount)
			}
		}
		who := e.Robot
		if who != "" {
			line = fmt.Sprintf("t%d [%s] %s", tick, who, line)
		} else {
			line = fmt.Sprintf("t%d %s", tick, line)
		}
		out = append(out, feedLine{Tick: tick, Line: line})
	}
	return out
}

// ---------------------------------------------------------------------------
// output
// ---------------------------------------------------------------------------

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

func (c *City) printSummary(s summaryData) {
	bar := "================================================" // 48 '='
	fmt.Println("")
	fmt.Println(bar)
	fmt.Println("SUMMARY")
	fmt.Println(bar)
	fmt.Printf("  world             : %s\n", c.describeWorld())
	// A resumed run continues the CITY'S tick numbering, so a bare "final tick"
	// would imply it started at zero. Show the range it actually covered.
	if c.resumed {
		fmt.Printf("  ticks run         : %d -> %d (%d ticks, continuing the city's own numbering)\n",
			c.saveTick+1, s.FinalTick, c.ticks)
	} else {
		fmt.Printf("  final tick        : %d\n", s.FinalTick)
	}
	fmt.Printf("  robots            : %d\n", s.Robots)
	fmt.Printf("  robots expired    : %d   (end of life — expected; build replacements)\n", s.RobotsExpired)
	fmt.Printf("  robots destroyed  : %d   (out of energy mid-flight — avoidable; check your charging)\n", s.RobotsDestroyed)
	if s.RobotsUnattributed > 0 {
		fmt.Printf("  robots lost (unattributed) : %d\n", s.RobotsUnattributed)
	}
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
	fmt.Printf("  base level        : %d\n", s.BaseLevel)
	fmt.Printf("  ore   (mined/stored): %d / %d\n", s.OreMined, s.OreStored)
	fmt.Printf("  metal (mined/stored): %d / %d\n", s.MetalMined, s.MetalStored)
	fmt.Printf("  spots found       : %d\n", s.SpotsFound)
	fmt.Printf("  discovered cells  : %d\n", s.DiscoveredCells)
	if d := c.readModelDrift(s); d != "" {
		fmt.Printf("  read model drift  : %s\n", d)
	}
	fmt.Printf("  handler errors    : %d", len(c.errors))
	if len(c.errors) > 0 {
		fmt.Printf("  <-- your controller raised (see above)")
	}
	fmt.Println("")
	// The verdict keys on handler errors + energy deaths ONLY. A fleet aging out
	// is normal and must never read as a fault.
	switch {
	case len(c.errors) > 0:
		fmt.Printf("LOCAL-CHECK: FAIL — %d handler error(s); see above\n", len(c.errors))
	case s.RobotsDestroyed > 0:
		fmt.Printf("LOCAL-CHECK: PASS — 0 handler errors, but %d robot(s) ran out of energy "+
			"mid-flight (cargo lost); %d expired at end of life, which is normal\n",
			s.RobotsDestroyed, s.RobotsExpired)
	default:
		fmt.Printf("LOCAL-CHECK: PASS — 0 handler errors, 0 robots destroyed "+
			"(%d expired at end of life, which is normal)\n", s.RobotsExpired)
	}
	fmt.Printf("LOCAL-CHECK: world — %s\n", c.describeWorld())
}

// readModelDrift reports any gap between what the handlers could SEE and what the
// engine actually HOLDS. On a resumed run the read model is seeded from the city's
// display state, read at the city's current tick, which can be newer than the
// checkpoint the engine restored. The engine's own stats are authoritative, so any
// disagreement is stated rather than passed off as fact. Empty when they agree.
func (c *City) readModelDrift(s summaryData) string {
	if !c.resumed {
		return ""
	}
	er, eb := s.EngineRobots, s.EngineBuildings
	if er == 0 && eb == 0 {
		return "" // the engine reported no stats this run
	}
	if er == s.Robots && eb == s.Buildings {
		return ""
	}
	return fmt.Sprintf("your handlers saw %d robots / %d buildings; the engine holds %d / %d "+
		"(the seeded display state was newer than the restored save)",
		s.Robots, s.Buildings, er, eb)
}

// describeWorld names the world this run used and where it came from (#73 req 2).
// Printed in the banner AND in the summary, so a substituted world cannot be
// mistaken for a normal run.
func (c *City) describeWorld() string {
	origin := c.worldOrigin
	if origin == "" {
		origin = "unspecified"
	}
	out := fmt.Sprintf("seed %d | %s", c.seed, origin)
	if c.resumed {
		out += fmt.Sprintf(" | %d saved store key(s) restored", len(c.initialStore))
	}
	if len(c.cityConfig) > 0 {
		keys := make([]string, 0, len(c.cityConfig))
		for k := range c.cityConfig {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out += " | config: " + strings.Join(keys, ", ")
	}
	start := c.worldStart
	if start == "" {
		start = "fresh world at tick 0"
	}
	return out + " | " + start
}

// jsonOut is the machine-readable document shape:
// {world,seed,ticks,city,summary,errors,feed}. `world` is there so an automated
// check can ASSERT which world produced the numbers (#73 req 2) — the banner is
// suppressed under --json, so without it a substitution would be invisible.
type jsonOut struct {
	World   jsonWorld      `json:"world"`
	Seed    int64          `json:"seed"`
	Ticks   int64          `json:"ticks"`
	City    string         `json:"city"`
	Summary jsonSummary    `json:"summary"`
	Errors  []handlerError `json:"errors"`
	Feed    []feedLine     `json:"feed"`
}

type jsonWorld struct {
	Seed      int64          `json:"seed"`
	City      string         `json:"city"`
	Origin    string         `json:"origin"`
	Config    map[string]any `json:"config,omitempty"`
	Start     string         `json:"start"`
	Resumed   bool           `json:"resumed"`
	StartTick int64          `json:"start_tick"`
	StoreKeys int            `json:"store_keys"`
}

type jsonResource struct {
	Mined  int `json:"mined"`
	Stored int `json:"stored"`
}

type jsonSummary struct {
	FinalTick          int64          `json:"final_tick"`
	Robots             int            `json:"robots"`
	RobotsExpired      int            `json:"robots_expired"`
	RobotsDestroyed    int            `json:"robots_destroyed"`
	RobotsUnattributed int            `json:"robots_removed_unattributed"`
	Buildings          int            `json:"buildings"`
	BuildingsByType    map[string]int `json:"buildings_by_type"`
	BaseLevel          int            `json:"base_level"`
	Ore                jsonResource   `json:"ore"`
	Metal              jsonResource   `json:"metal"`
	SpotsFound         int            `json:"spots_found"`
	DiscoveredCells    int            `json:"discovered_cells"`
}

func (c *City) printJSON(s summaryData, feed []feedLine) {
	if feed == nil {
		feed = []feedLine{}
	}
	out := jsonOut{
		World: jsonWorld{
			Seed: c.seed, City: c.id, Origin: c.worldOrigin,
			Config: c.cityConfig, Start: c.worldStart,
			Resumed: c.resumed, StartTick: c.saveTick, StoreKeys: len(c.initialStore),
		},
		Seed:  c.seed,
		Ticks: c.ticks,
		City:  c.id,
		Summary: jsonSummary{
			FinalTick:          s.FinalTick,
			Robots:             s.Robots,
			RobotsExpired:      s.RobotsExpired,
			RobotsDestroyed:    s.RobotsDestroyed,
			RobotsUnattributed: s.RobotsUnattributed,
			Buildings:          s.Buildings,
			BuildingsByType:    s.BuildingsByType,
			BaseLevel:          s.BaseLevel,
			Ore:                jsonResource{Mined: s.OreMined, Stored: s.OreStored},
			Metal:              jsonResource{Mined: s.MetalMined, Stored: s.MetalStored},
			SpotsFound:         s.SpotsFound,
			DiscoveredCells:    s.DiscoveredCells,
		},
		Errors: append([]handlerError{}, c.errors...),
		Feed:   feed,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Fprintln(os.Stdout, string(b))
}
