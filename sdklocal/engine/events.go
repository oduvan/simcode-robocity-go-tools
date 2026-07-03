package engine

import (
	"encoding/json"
	"fmt"
	"math"
)

// Event names (GAME -> script). Mirror of contract.AllEvents.
const (
	EventSpawn                = "spawn"
	EventTick                 = "tick"
	EventIdle                 = "idle"
	EventArrived              = "arrived"
	EventBlocked              = "blocked"
	EventConstructionStarted  = "construction_started"
	EventResourceDelivered    = "resource_delivered"
	EventConstructionComplete = "construction_complete"
	EventSpotDepleted         = "spot_depleted"
	EventStorageFull          = "storage_full"
	EventInventoryFull        = "inventory_full"
	EventRobotProduced        = "robot_produced"
	EventRobotDestroyed       = "robot_destroyed"
	EventChargeComplete       = "charge_complete"
	EventMessage              = "message"
)

// Command names (script -> GAME). Mirror of contract.AllCommands.
const (
	CmdMoveTo     = "move_to"
	CmdPickUp     = "pick_up"
	CmdDrop       = "drop"
	CmdCharge     = "charge"
	CmdSend       = "send"
	CmdCancel     = "cancel"
	CmdBuild      = "build"
	CmdBuildRobot = "build_robot"
	CmdBaseCancel = "base_cancel"
)

// allCommands is the set of robot/world/base commands the engine accepts.
var allCommands = map[string]bool{
	CmdMoveTo: true, CmdPickUp: true, CmdDrop: true, CmdCharge: true,
	CmdSend: true, CmdCancel: true, CmdBuild: true, CmdBuildRobot: true,
	CmdBaseCancel: true,
}

// FeedKindLog marks a feed entry that carries a user r.Log() line in Text.
const FeedKindLog = "log"

// Command is one {cmd, args} entry from an intent. args are native values.
type Command struct {
	Cmd  string
	Args []any
}

// Intent is CODE -> GAME: the commands a handler issued for one robot, plus logs.
type Intent struct {
	Robot    string
	Commands []Command
	Logs     []string
}

// Event is GAME -> CODE. Payload is the event's read-only extra data as JSON
// (unmarshalled to map[string]any by the driver, mirroring the wire path).
type Event struct {
	Event   string
	Robot   string
	Tick    int64
	Payload json.RawMessage
}

// FeedEvent is one entry in the activity feed. Line() renders it exactly like the
// server's contract.FeedEvent.Line (docs/… protocol) so output matches the tools.
type FeedEvent struct {
	Kind     string
	Robot    string
	Resource string
	Amount   int
	Text     string
	Tick     int64
}

// Line renders a feed entry as one human-readable log line.
//
//	t549 r2: charged — heading out to explore   (a user r.Log() line)
//	t548 r2 charge_complete                      (a game event)
func (f FeedEvent) Line() string {
	who := f.Robot
	if f.Kind == FeedKindLog {
		if who != "" {
			who += ": "
		}
		return fmt.Sprintf("t%d %s%s", f.Tick, who, f.Text)
	}
	if who != "" {
		who += " "
	}
	line := fmt.Sprintf("t%d %s%s", f.Tick, who, f.Kind)
	if f.Amount != 0 {
		line += fmt.Sprintf(" %d", f.Amount)
	}
	if f.Resource != "" {
		line += " " + f.Resource
	}
	return line
}

// ev constructs an Event, marshalling payload to JSON.
func ev(name, robot string, tick int64, payload any) Event {
	var raw json.RawMessage
	if payload != nil {
		b, _ := json.Marshal(payload)
		raw = b
	}
	return Event{Event: name, Robot: robot, Tick: tick, Payload: raw}
}

// --- arg helpers (args are already-decoded native values, mirroring the Python
// port). We accept int/int64/float64 where a number is expected. bool is never a
// number. This matches the Go server's json-based argInt/argFloat/optInt exactly
// for the value types the SDK produces. ---

func numOf(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func argFloat(args []any, i int, def float64) float64 {
	if i < 0 || i >= len(args) {
		return def
	}
	if f, ok := numOf(args[i]); ok {
		return f
	}
	return def
}

func argInt(args []any, i, def int) int {
	if i < 0 || i >= len(args) {
		return def
	}
	if f, ok := numOf(args[i]); ok {
		return int(f) // truncates, matching Go argInt
	}
	return def
}

// optInt mirrors Go optInt: ok only when the value is an integer (a float64 that
// carries a fractional part, or any non-number, is rejected). SDK PickUp/Drop
// always pass ints, so this is ok in practice; a JSON-seeded float is rejected.
func optInt(args []any, i int) (int, bool) {
	if i < 0 || i >= len(args) {
		return 0, false
	}
	// Mirror Go optInt (json.Unmarshal into int): a plain integer succeeds; a
	// float literal like 5.0 fails. The SDK PickUp/Drop always pass ints.
	switch n := args[i].(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		if iv, err := n.Int64(); err == nil {
			return int(iv), true
		}
		return 0, false
	}
	return 0, false
}

func argStr(args []any, i int, def string) string {
	if i < 0 || i >= len(args) {
		return def
	}
	if s, ok := args[i].(string); ok {
		return s
	}
	return def
}

// faceOfF returns the cardinal heading of a flight delta (origin top-left, +y down).
func faceOfF(dx, dy float64) string {
	if math.Abs(dx) >= math.Abs(dy) {
		if dx >= 0 {
			return "E"
		}
		return "W"
	}
	if dy >= 0 {
		return "S"
	}
	return "N"
}
