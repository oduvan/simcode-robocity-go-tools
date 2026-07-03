// The engine-backed City: same public API as the published SDK
// (github.com/lyabah/simcode-sdk-go), but the runtime drives an IN-PROCESS engine
// instead of Redis. New() builds a canonical (or from-live) world; On registers
// handlers; Run() runs the local tick loop (see driver.go) and prints the feed +
// SUMMARY (or JSON). The untrusted user script sees exactly the published API.
package simcode

import (
	"os"
	"strconv"

	"github.com/oduvan/simcode-robocity-go-tools/sdklocal/engine"
)

// Handler reacts to one event by issuing commands through the read model.
type Handler func(Event)

// City is the user-facing entry point: register handlers with On, then Run.
type City struct {
	id  string
	eng *engine.Module

	handlers map[string][]Handler
	order    []string

	acc  *accumulator
	snap snapshot // current published state; Robot/World/etc. read this

	storeState  map[string]any
	memoryState map[string]map[string]any

	// sim config (read from the environment the CLI sets)
	ticks int64
	seed  int64
	json  bool
	quiet bool
}

// New builds a City with an in-process engine seeded from the environment the
// robocity-sim CLI sets (ROBOCITY_SIM_SEED / _TICKS / _JSON / _QUIET / _CITY /
// _LIVE). Defaults: seed 7 (canonical), 500 ticks, city "local". New never fails
// so user code can write `city := sc.New()`.
func New() *City {
	c := &City{
		handlers:    map[string][]Handler{},
		acc:         newAccumulator(),
		storeState:  map[string]any{},
		memoryState: map[string]map[string]any{},
		id:          envOr("ROBOCITY_SIM_CITY", "local"),
		ticks:       envInt64("ROBOCITY_SIM_TICKS", 500),
		seed:        envInt64("ROBOCITY_SIM_SEED", engine.CanonicalSeed),
		json:        os.Getenv("ROBOCITY_SIM_JSON") == "1",
		quiet:       os.Getenv("ROBOCITY_SIM_QUIET") == "1",
	}

	c.eng = engine.New()

	// --from-live: overlay a fetched public snapshot on a fresh world.
	if path := os.Getenv("ROBOCITY_SIM_LIVE"); path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			if snap, perr := engine.ParseLiveSnapshot(raw); perr == nil {
				if snap.World.Seed != 0 {
					c.seed = snap.World.Seed
				}
				c.eng.ResetWorld(c.id, c.seed)
				c.eng.SeedFromSnapshot(snap)
				return c
			}
		}
	}

	c.eng.ResetWorld(c.id, c.seed)
	return c
}

// On registers an event handler. Multiple handlers per event fire in
// registration order. Call before Run.
func (c *City) On(event string, handler Handler) {
	if _, seen := c.handlers[event]; !seen {
		c.order = append(c.order, event)
	}
	c.handlers[event] = append(c.handlers[event], handler)
}

// ---- read model (backed by the current published snapshot) ----

func (c *City) readSnapshot() snapshot { return c.snap }

// Robot returns a fresh handle to one robot.
func (c *City) Robot(id string) *Robot {
	return &Robot{ID: id, city: c, snap: c.snap, data: c.snap.robots[id]}
}

// Buildings returns a fresh handle for every building in the city.
func (c *City) Buildings() []*Building {
	out := make([]*Building, 0, len(c.snap.buildings))
	for id := range c.snap.buildings {
		b := c.snap.buildings[id]
		out = append(out, &Building{ID: id, city: c, data: b})
	}
	return out
}

// Base returns the city's Base building, or nil if not present yet.
func (c *City) Base() *Building {
	for id, b := range c.snap.buildings {
		if b.Type == BuildingBase {
			return &Building{ID: id, city: c, data: c.snap.buildings[id]}
		}
	}
	return nil
}

// World returns a fresh read of the world header + revealed cells.
func (c *City) World() World { return World{snap: c.snap, city: c} }

// ---- store / memory (in-process; recorded onto intents) ----

// GetStore reads a city-wide store value.
func (c *City) GetStore(key string) (any, bool) {
	v, ok := c.storeState[key]
	return v, ok
}

// SetStore writes a city-wide store value.
func (c *City) SetStore(key string, value any) {
	c.storeState[key] = value
	c.acc.setStore(key, value)
}

func (c *City) robotMemory(id string) map[string]any {
	m, ok := c.memoryState[id]
	if !ok {
		m = map[string]any{}
		c.memoryState[id] = m
	}
	return m
}

func (c *City) setRobotMemory(id string, mem map[string]any) {
	c.memoryState[id] = mem
}

// ---- dispatch (data-in / intents-out) ----

// dispatch handles one event: reset the accumulator, run every subscribed handler
// (a panic in one handler must not kill the loop), then flush intents. Mirrors the
// published runtime, minus Redis publish. Returns the intent envelopes.
func (c *City) dispatch(ev Event) []intentEnvelope {
	handlers := c.handlers[ev.Event]
	if len(handlers) == 0 {
		return nil
	}
	c.acc = newAccumulator()
	for _, h := range handlers {
		c.runHandler(h, ev)
	}
	return c.acc.buildIntents(c.id, ev.Robot)
}

func (c *City) runHandler(h Handler, ev Event) {
	defer func() { _ = recover() }() // isolate handler panics, like the server
	h(ev)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
