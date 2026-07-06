// Read-model handles + command methods. Copied verbatim from the published SDK
// (github.com/lyabah/simcode-sdk-go, handles.go). A handle is a thin view over a
// freshly-read snapshot; commands issued through it are *recorded* on the city's
// active accumulator (data-in / intents-out), not executed directly. The local
// driver later feeds the accumulated intents to the in-process engine.
package simcode

import "math"

// roundCell rounds a continuous coordinate to its integer grid cell.
func roundCell(v float64) int { return int(math.Round(v)) }

// ----------------------------------------------------------------------------
// Robot
// ----------------------------------------------------------------------------

// Robot is a handle to one robot: read its live state, issue its next command.
type Robot struct {
	ID   string
	city *City
	snap snapshot
	data robotState
}

// Position returns the robot's continuous position (0,0 if unknown).
func (r *Robot) Position() (float64, float64) {
	if r.data.Pos != nil {
		return r.data.Pos[0], r.data.Pos[1]
	}
	return 0, 0
}

// Cell returns the robot's rounded integer cell.
func (r *Robot) Cell() (int, int) {
	x, y := r.Position()
	return roundCell(x), roundCell(y)
}

// Type is the robot kind.
func (r *Robot) Type() string { return r.data.Type }

// Facing is the robot's heading (N|S|E|W).
func (r *Robot) Facing() string { return r.data.Facing }

// State is the robot's current activity (idle|moving|charging|hauling|blocked).
func (r *Robot) State() string { return r.data.State }

// Command is the robot's active command name, if any.
func (r *Robot) Command() string { return r.data.Command }

// Energy is the robot's flight battery (0 if unknown).
func (r *Robot) Energy() float64 {
	if r.data.Energy != nil {
		return *r.data.Energy
	}
	return 0
}

// Inventory is the robot's carried resources.
func (r *Robot) Inventory() Inventory {
	if r.data.Inventory != nil {
		return *r.data.Inventory
	}
	return Inventory{}
}

// Here describes the robot's current cell (terrain / spot / building).
type Here struct {
	X, Y     int
	Terrain  string
	Spot     *Spot
	Building *Building
}

// Here returns what is on the robot's current cell (its rounded position).
func (r *Robot) Here() Here {
	x, y := r.Cell()
	h := Here{X: x, Y: y}
	if t, ok := r.snap.tileAt(x, y); ok {
		h.Terrain = t.Terrain
		h.Spot = t.Spot
	}
	if b := r.snap.buildingAt(x, y); b != nil {
		h.Building = &Building{ID: b.ID, city: r.city, data: *b}
	}
	return h
}

// ----- commands (intents-out): positional args in the engine's arg order -----

func (r *Robot) emit(cmd string, args ...any) *Robot {
	r.city.acc.addCommand(r.ID, makeCommand(cmd, args...))
	return r
}

// MoveTo flies the robot in a straight line to (x, y).
func (r *Robot) MoveTo(x, y float64) *Robot { return r.emit(CmdMoveTo, x, y) }

// Charge recharges the robot's battery while it is parked on a Flying Station.
func (r *Robot) Charge() *Robot { return r.emit(CmdCharge) }

// PickUp loads resource from the current cell. No args picks up all available.
func (r *Robot) PickUp(amounts ...int) *Robot { return r.transfer(CmdPickUp, amounts) }

// Drop unloads resource onto the current cell. No args drops all.
func (r *Robot) Drop(amounts ...int) *Robot { return r.transfer(CmdDrop, amounts) }

func (r *Robot) transfer(cmd string, amounts []int) *Robot {
	if len(amounts) == 0 {
		return r.emit(cmd)
	}
	ore := amounts[0]
	metal := 0
	if len(amounts) > 1 {
		metal = amounts[1]
	}
	return r.emit(cmd, ore, metal)
}

// Send delivers a payload message to another robot.
func (r *Robot) Send(targetID string, payload any) *Robot {
	return r.emit(CmdSend, targetID, payload)
}

// Cancel stops the robot's active command.
func (r *Robot) Cancel() *Robot { return r.emit(CmdCancel) }

// Log records a line on this robot's intent (surfaced in the city's logs).
func (r *Robot) Log(msg string) *Robot {
	r.city.acc.addLog(r.ID, msg)
	return r
}

// Memory returns this robot's in-process scratch dict.
func (r *Robot) Memory() map[string]any {
	return r.city.robotMemory(r.ID)
}

// SetMemory replaces this robot's memory and records it on the intent.
func (r *Robot) SetMemory(mem map[string]any) *Robot {
	r.city.setRobotMemory(r.ID, mem)
	r.city.acc.setMemory(r.ID, copyMap(mem))
	return r
}

// ----------------------------------------------------------------------------
// Building
// ----------------------------------------------------------------------------

// Building is a handle to one building.
type Building struct {
	ID   string
	city *City
	data buildingState
}

// Type is the building kind (base|mining|storage|flying_station).
func (b *Building) Type() string { return b.data.Type }

// Position returns the building's grid position (0,0 if unknown).
func (b *Building) Position() (int, int) {
	if b.data.Pos != nil {
		return b.data.Pos[0], b.data.Pos[1]
	}
	return 0, 0
}

// Status is the building's status (constructing|active).
func (b *Building) Status() string { return b.data.Status }

// Storage is the building's stored resources (zero value if none).
func (b *Building) Storage() Storage {
	if b.data.Storage != nil {
		return *b.data.Storage
	}
	return Storage{}
}

// Spot is the resource deposit under the building, if any.
func (b *Building) Spot() *Spot { return b.data.Spot }

// Production is a Flying Station's robot-production status (raw attribute bag).
func (b *Building) Production() map[string]any { return b.data.Production }

// Construction is the in-progress recipe on a constructing building (raw bag).
func (b *Building) Construction() map[string]any { return b.data.Construction }

// Level is the Base's current objective level (1+). 0 for non-Base buildings.
func (b *Building) Level() int { return b.data.Level }

// Quest is the Base's current quest: {required:{ore,metal}, progress:{ore,metal}}.
// Nil for non-Base buildings.
func (b *Building) Quest() map[string]any { return b.data.Quest }

// BuildRobot queues n robots built at THIS Flying Station. The command targets
// this building's id; the engine rejects a non-station target with a `blocked`
// reason `not_a_station`. Each queued unit consumes the robot recipe from this
// station's own production store and spawns at the station (empty, full energy).
func (b *Building) BuildRobot(n int) *Building {
	b.city.acc.addCommand(b.ID, makeCommand(CmdBuildRobot, n))
	return b
}

// Cancel stops THIS Flying Station's production queue (an in-progress unit still
// finishes).
func (b *Building) Cancel() *Building {
	b.city.acc.addCommand(b.ID, makeCommand(CmdBaseCancel))
	return b
}

// ----------------------------------------------------------------------------
// World
// ----------------------------------------------------------------------------

// World is the read-only world header + revealed cells, plus world-scoped build orders.
type World struct {
	snap snapshot
	city *City
}

// Tick is the current simulation tick.
func (w World) Tick() int64 { return w.snap.meta.Tick }

// Seq is the monotonic state sequence number.
func (w World) Seq() int64 { return w.snap.meta.Seq }

// Size returns the discovered bounding-box extent (w, h); (0,0) if nothing revealed.
func (w World) Size() (int, int) {
	if w.snap.world.Size != nil {
		return w.snap.world.Size[0], w.snap.world.Size[1]
	}
	return 0, 0
}

// Origin returns the min (x,y) of the discovered region.
func (w World) Origin() (int, int) {
	if w.snap.world.Origin != nil {
		return w.snap.world.Origin[0], w.snap.world.Origin[1]
	}
	return 0, 0
}

// Endless reports whether the world has no fixed bounds (always true here).
func (w World) Endless() bool { return w.snap.world.Endless }

// Seed is the world generation seed.
func (w World) Seed() int64 { return w.snap.world.Seed }

// Build places a construction site of the given type at (x, y) — a world-scoped order.
func (w World) Build(buildingType string, x, y int) World {
	if w.city != nil {
		w.city.acc.addCommand("world", makeCommand(CmdBuild, buildingType, x, y))
	}
	return w
}

// Discovered is the raw revealed-cell data (a JSON list of [x,y]).
func (w World) Discovered() string { return w.snap.discovered }

// Cell is a revealed tile.
type Cell struct {
	X, Y    int
	Terrain string
	Spot    *Spot
}

// Spots returns every revealed cell that holds a resource spot.
func (w World) Spots() []Cell {
	var out []Cell
	for _, t := range w.snap.tiles {
		if t.Spot != nil {
			out = append(out, Cell{X: t.X, Y: t.Y, Terrain: t.Terrain, Spot: t.Spot})
		}
	}
	return out
}
