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

// Inventory is the robot's carried resources (a multi-item Store; zero value
// with a non-nil empty map when the robot carries nothing).
func (r *Robot) Inventory() Store {
	return storeOrEmpty(r.data.Inventory)
}

// LifeRemaining is the robot's remaining cumulative flight distance before it
// expires (robot_expired). 0 if unknown. Retire/replace robots before it runs
// out. #42.
func (r *Robot) LifeRemaining() float64 {
	if r.data.LifeRemaining != nil {
		return *r.data.LifeRemaining
	}
	return 0
}

// LifeMax is the robot's total lifespan (max cumulative flight distance) for its
// type. 0 if unknown. #42.
func (r *Robot) LifeMax() float64 {
	if r.data.LifeMax != nil {
		return *r.data.LifeMax
	}
	return 0
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

// Repair (Mechanic only) starts a repair process on the worn building on the
// robot's current cell, draining the robot's held metal into the building's
// condition until it runs dry or the building is full (repair_complete). No
// args — like Charge, it targets whatever building the robot sits on. #42.
func (r *Robot) Repair() *Robot { return r.emit(CmdRepair) }

// PickUp loads `amount` of `item` from the current cell (wire args [item, amount]).
func (r *Robot) PickUp(item string, amount int) *Robot { return r.emit(CmdPickUp, item, amount) }

// PickUpItem loads all available of a single `item` (wire args [item]).
func (r *Robot) PickUpItem(item string) *Robot { return r.emit(CmdPickUp, item) }

// PickUpAll loads all available of every item (wire args []).
func (r *Robot) PickUpAll() *Robot { return r.emit(CmdPickUp) }

// Drop unloads `amount` of `item` onto the current cell (wire args [item, amount]).
func (r *Robot) Drop(item string, amount int) *Robot { return r.emit(CmdDrop, item, amount) }

// DropItem unloads all carried of a single `item` (wire args [item]).
func (r *Robot) DropItem(item string) *Robot { return r.emit(CmdDrop, item) }

// DropAll unloads the robot's entire inventory (wire args []).
func (r *Robot) DropAll() *Robot { return r.emit(CmdDrop) }

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

// Storage is the building's stored resources (a multi-item Store; zero value
// with a non-nil empty map when the building stores nothing).
func (b *Building) Storage() Store {
	return storeOrEmpty(b.data.Storage)
}

// Input is a processor's input pool — where haulers drop() raw feedstock — or
// nil on a building with no input store (Base/Storage/Station). The returned
// *Store has a non-nil Items map when present.
func (b *Building) Input() *Store { return storePtr(b.data.Input) }

// Output is a processor's output pool — where haulers pick_up() finished goods —
// or nil on a building with no output store.
func (b *Building) Output() *Store { return storePtr(b.data.Output) }

// Recoverable is the haulable materials store a building exposes while
// decommissioning (its build cost refund + current contents); nil otherwise.
func (b *Building) Recoverable() *Store { return storePtr(b.data.Recoverable) }

// Recipe is a processor's fixed conversion (inputs → output×amount over ticks),
// or nil on a non-processor building.
func (b *Building) Recipe() *Recipe {
	rv := b.data.Recipe
	if rv == nil {
		return nil
	}
	return &Recipe{inputs: rv.Inputs, output: rv.Output, outAmount: rv.OutAmount, ticks: rv.Ticks}
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

// Condition is a wearing T2/T3 processor's condition meter (0-100); productivity
// scales with it and it stops producing at 0. Returns nil on buildings that never
// wear (base/mining/T1/storage/station). A Mechanic tops it up via Repair. #42.
func (b *Building) Condition() *int { return b.data.Condition }

// Unlocks is the set of building + robot types buildable at the Base's current
// level (nil on non-Base buildings, or a Base whose state omits it). #42.
func (b *Building) Unlocks() []string { return b.data.Unlocks }

// Recipe is a read-only view of a processor building's fixed conversion.
type Recipe struct {
	inputs    map[string]int
	output    string
	outAmount int
	ticks     int
}

// Inputs are the items (item → qty) consumed per batch (non-nil, possibly empty).
func (r *Recipe) Inputs() map[string]int {
	if r.inputs == nil {
		return map[string]int{}
	}
	return r.inputs
}

// Output is the item name this recipe produces.
func (r *Recipe) Output() string { return r.output }

// OutAmount is how many of the output item one batch yields.
func (r *Recipe) OutAmount() int { return r.outAmount }

// Ticks is how many simulation ticks one batch takes.
func (r *Recipe) Ticks() int { return r.ticks }

// BuildRobot queues n robots of robotType built at THIS Flying Station (wire args
// [type, n]). The command targets this building's id; the engine rejects a
// non-station target (blocked reason not_a_station) and a type unlocked above the
// Base's level (blocked reason level_required). An empty robotType defaults to
// RobotBuilder (the starting class); n < 1 clamps to 1. #42. The robot types are
// RobotBuilder / RobotHauler / RobotScout / RobotMechanic / RobotHeavyHauler /
// RobotRanger. Each queued unit consumes the robot recipe from this station's own
// production store and spawns at the station (empty, full energy).
func (b *Building) BuildRobot(robotType string, n int) *Building {
	if robotType == "" {
		robotType = RobotBuilder
	}
	if n < 1 {
		n = 1
	}
	b.city.acc.addCommand(b.ID, makeCommand(CmdBuildRobot, robotType, n))
	return b
}

// Cancel stops THIS Flying Station's production queue (an in-progress unit still
// finishes).
func (b *Building) Cancel() *Building {
	b.city.acc.addCommand(b.ID, makeCommand(CmdBaseCancel))
	return b
}

// Destroy decommissions THIS building. Destroy is world-scoped (args [x, y]), so
// it targets the building's current position — a convenience wrapper over
// World().Destroy for when you already hold the handle.
func (b *Building) Destroy() *Building {
	x, y := b.Position()
	b.city.acc.addCommand("world", makeCommand(CmdDestroy, x, y))
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

// Destroy decommissions the building at (x, y) — a world-scoped order (sibling of
// Build). The building enters `decommissioning`: its build-cost refund plus
// current contents become a recoverable store robots haul to Storage; once empty
// it is removed and a building_destroyed event fires.
func (w World) Destroy(x, y int) World {
	if w.city != nil {
		w.city.acc.addCommand("world", makeCommand(CmdDestroy, x, y))
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
