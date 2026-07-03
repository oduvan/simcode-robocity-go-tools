// Frozen event / command / enum names — the Go mirror of game/core/contract.
// Copied verbatim from the published SDK (github.com/lyabah/simcode-sdk-go) so
// user code that references sc.EventIdle, sc.CmdMoveTo, sc.BuildingMining, etc.
// compiles unchanged against this local, engine-backed SDK.
package simcode

// Envelope type tags (the "type" field on every message).
const (
	TypeDelta     = "delta"
	TypeEvent     = "event"
	TypeIntent    = "intent"
	TypeControl   = "control"
	TypeSubscribe = "subscribe"
	TypeSnapshot  = "snapshot"
)

// Event names (GAME -> script).
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

// Command names (script -> GAME).
const (
	CmdMoveTo     = "move_to"
	CmdPickUp     = "pick_up"
	CmdDrop       = "drop"
	CmdCharge     = "charge"
	CmdSend       = "send"
	CmdCancel     = "cancel"
	CmdBuild      = "build" // world-scoped (World.Build)
	CmdBuildRobot = "build_robot"
	CmdBaseCancel = "base_cancel"
)

// Building type enum (also the world.build type argument values).
const (
	BuildingBase          = "base"
	BuildingMining        = "mining"
	BuildingStorage       = "storage"
	BuildingFlyingStation = "flying_station"
)

// Robot state enum.
const (
	StateIdle     = "idle"
	StateMoving   = "moving"
	StateCharging = "charging"
	StateHauling  = "hauling"
	StateBlocked  = "blocked"
)

// Building status enum.
const (
	StatusConstructing = "constructing"
	StatusActive       = "active"
)

// Well-known state KV sub-keys (mirror of the runtime's _STATE_KEYS).
const (
	StateMeta       = "meta"
	StateWorld      = "world"
	StateRobots     = "robots"
	StateBuildings  = "buildings"
	StateTiles      = "tiles"
	StateStats      = "stats"
	StateDiscovered = "discovered"
)

// stateKeys is the order the read-model snapshot is assembled in (matches the
// published SDK / Python runtime _STATE_KEYS; order matters for decodeSnapshot).
var stateKeys = []string{
	StateMeta, StateWorld, StateRobots, StateBuildings, StateTiles, StateDiscovered,
}
