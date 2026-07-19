// Wire envelopes + the per-event accumulator. Copied from the published SDK
// (github.com/lyabah/simcode-sdk-go, contract.go), minus the Redis-only subscribe
// envelope. The Event view and the intent/accumulator batching are byte-identical
// to the server contract, so the local run reproduces production intent ordering.
package simcode

import (
	"encoding/json"
	"sort"
)

// Event is one event delivered to a handler. Payload keys are exposed through Get;
// the handler reads live world state from the read model, not the payload.
type Event struct {
	City    string         `json:"city"`
	Type    string         `json:"type"`
	Event   string         `json:"event"`
	Robot   string         `json:"robot"`
	Tick    int64          `json:"tick"`
	Payload map[string]any `json:"payload,omitempty"`
}

// Get returns a payload field (then a top-level envelope field) or nil.
func (e Event) Get(key string) any {
	if e.Payload != nil {
		if v, ok := e.Payload[key]; ok {
			return v
		}
	}
	switch key {
	case "city":
		return e.City
	case "event":
		return e.Event
	case "robot":
		return e.Robot
	case "tick":
		return e.Tick
	}
	return nil
}

// BuildingID returns the building this event addresses (for building-addressed
// supply-chain events like resource_produced / production_blocked /
// building_destroyed / decommission_started), or "" if the payload carries none.
func (e Event) BuildingID() string { return e.getString("building_id") }

// RobotID returns the robot a robot-addressed payload names — for events that
// carry the id in the payload rather than the envelope (robot_expired,
// repair_complete). Falls back to the envelope robot field if the payload has none.
func (e Event) RobotID() string {
	if id := e.getString("robot_id"); id != "" {
		return id
	}
	return e.Robot
}

// Condition returns the building condition (0-100) a maintenance_needed /
// repair_complete event carries (0 if the payload has none). #42.
func (e Event) Condition() int { return e.getInt("condition") }

// Item returns the item name a resource_produced event carries ("" if absent).
func (e Event) Item() string { return e.getString("item") }

// Amount returns the quantity a resource_produced event carries (0 if absent).
func (e Event) Amount() int { return e.getInt("amount") }

// Reason returns a production_blocked event's reason (e.g. "output_full" |
// "input_short"), or "" if the payload carries none.
func (e Event) Reason() string { return e.getString("reason") }

// getString reads a payload/envelope field as a string ("" if missing/wrong type).
func (e Event) getString(key string) string {
	if s, ok := e.Get(key).(string); ok {
		return s
	}
	return ""
}

// getInt reads a payload field as an int. JSON numbers decode to float64, so
// accept both (0 if missing/wrong type).
func (e Event) getInt(key string) int {
	switch v := e.Get(key).(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

// decodeEvent parses a raw event envelope (kept for parity/testing).
func decodeEvent(raw []byte) (Event, error) {
	var ev Event
	err := json.Unmarshal(raw, &ev)
	return ev, err
}

// command is one {cmd, args} entry. args is always present (possibly []).
type command struct {
	Cmd  string `json:"cmd"`
	Args []any  `json:"args"`
}

func makeCommand(cmd string, args ...any) command {
	if args == nil {
		args = []any{}
	}
	return command{Cmd: cmd, Args: args}
}

// intentEnvelope is CODE -> GAME. commands is always present; logs/store/memory
// are omitted when empty. Mirrors the published SDK exactly.
type intentEnvelope struct {
	City     string         `json:"city"`
	Type     string         `json:"type"`
	Robot    string         `json:"robot"`
	Commands []command      `json:"commands"`
	Logs     []string       `json:"logs,omitempty"`
	Store    map[string]any `json:"store,omitempty"`
	Memory   map[string]any `json:"memory,omitempty"`
}

// accumulator collects commands / logs / memory / store writes produced while
// handlers run for one event, then flushes them into one intent per target.
type accumulator struct {
	commands    map[string][]command
	logs        map[string][]string
	memory      map[string]map[string]any
	storeWrites map[string]any
}

func newAccumulator() *accumulator {
	return &accumulator{
		commands:    map[string][]command{},
		logs:        map[string][]string{},
		memory:      map[string]map[string]any{},
		storeWrites: map[string]any{},
	}
}

func (a *accumulator) addCommand(target string, c command) {
	a.commands[target] = append(a.commands[target], c)
}

func (a *accumulator) addLog(target, msg string) {
	a.logs[target] = append(a.logs[target], msg)
}

func (a *accumulator) setMemory(target string, mem map[string]any) {
	a.memory[target] = mem
}

func (a *accumulator) setStore(key string, value any) {
	a.storeWrites[key] = value
}

// buildIntents flushes the accumulator into intent envelopes. The event's own
// robot (primary) goes first, then the rest sorted; the city-wide store rides on
// the first intent emitted. Mirrors Accumulator.build_intents exactly.
func (a *accumulator) buildIntents(city, primary string) []intentEnvelope {
	targetSet := map[string]struct{}{}
	for t := range a.commands {
		targetSet[t] = struct{}{}
	}
	for t := range a.logs {
		targetSet[t] = struct{}{}
	}
	for t := range a.memory {
		targetSet[t] = struct{}{}
	}

	var rest []string
	hasPrimary := false
	for t := range targetSet {
		if t == primary {
			hasPrimary = true
			continue
		}
		rest = append(rest, t)
	}
	sort.Strings(rest)

	var ordered []string
	if hasPrimary {
		ordered = append(ordered, primary)
	}
	ordered = append(ordered, rest...)

	var intents []intentEnvelope
	storeEmitted := false
	for _, t := range ordered {
		cmds := a.commands[t]
		if cmds == nil {
			cmds = []command{}
		}
		env := intentEnvelope{
			City:     city,
			Type:     TypeIntent,
			Robot:    t,
			Commands: cmds,
			Logs:     a.logs[t],
			Memory:   a.memory[t],
		}
		if !storeEmitted && len(a.storeWrites) > 0 {
			env.Store = copyMap(a.storeWrites)
			storeEmitted = true
		}
		intents = append(intents, env)
	}

	if len(a.storeWrites) > 0 && !storeEmitted {
		intents = append(intents, intentEnvelope{
			City:     city,
			Type:     TypeIntent,
			Robot:    primary,
			Commands: []command{},
			Store:    copyMap(a.storeWrites),
		})
	}
	return intents
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
