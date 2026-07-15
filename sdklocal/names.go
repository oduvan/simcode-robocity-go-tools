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
	EventBaseLevelUp          = "base_level_up"
	EventQuestUpdated         = "quest_updated"
	// Supply-chain (#5) building-addressed events (empty robot; payload carries
	// building_id). They do not overload robot idle.
	EventResourceProduced    = "resource_produced"    // {building_id, item, amount}
	EventProductionBlocked   = "production_blocked"   // {building_id, reason}
	EventBuildingDestroyed   = "building_destroyed"   // {building_id}
	EventDecommissionStarted = "decommission_started" // {building_id}
)

// Command names (script -> GAME).
const (
	CmdMoveTo     = "move_to"
	CmdPickUp     = "pick_up"
	CmdDrop       = "drop"
	CmdCharge     = "charge"
	CmdSend       = "send"
	CmdCancel     = "cancel"
	CmdBuild      = "build"   // world-scoped (World.Build)
	CmdDestroy    = "destroy" // world-scoped (World.Destroy), args [x, y]
	CmdBuildRobot = "build_robot"
	CmdBaseCancel = "base_cancel"
)

// Building type enum (also the world.build type argument values).
const (
	BuildingBase          = "base"
	BuildingMining        = "mining"
	BuildingStorage       = "storage"
	BuildingFlyingStation = "flying_station"
	// Supply-chain (#5) processor + advanced building types.
	BuildingSmelter         = "smelter"          // ore   -> plate   (T1)
	BuildingWireMill        = "wire_mill"        // metal -> wire    (T1)
	BuildingGlassworks      = "glassworks"       // crystal -> glass (T1)
	BuildingKiln            = "kiln"             // carbon -> coke   (T1)
	BuildingAssembler       = "assembler"        // plate+wire -> part      (T2)
	BuildingElectronicsLab  = "electronics_lab"  // wire+glass -> circuit   (T2)
	BuildingAlloyFurnace    = "alloy_furnace"    // plate+coke -> alloy     (T2)
	BuildingModuleAssembler = "module_assembler" // part+circuit -> module  (T3)
	BuildingFrameShop       = "frame_shop"       // alloy+plate -> frame    (T3)
	BuildingDeepMine        = "deep_mine"        // upgraded mining
	BuildingWarehouse       = "warehouse"        // upgraded storage
	BuildingChargingTower   = "charging_tower"   // upgraded station
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
	StatusConstructing    = "constructing"
	StatusActive          = "active"
	StatusDecommissioning = "decommissioning" // #5: torn down, materials recoverable
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
