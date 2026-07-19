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
	// Living economy (#42): fleet expiry + building maintenance. robot_expired is
	// end-of-life (distinct from the avoidable robot_destroyed energy-death); the
	// three building events track a wearing T2/T3 processor's condition + repair.
	EventRobotExpired      = "robot_expired"      // {robot_id}
	EventMaintenanceNeeded = "maintenance_needed" // {building_id, condition}
	EventBuildingStopped   = "building_stopped"   // {building_id}
	EventRepairComplete    = "repair_complete"    // {building_id, robot_id, condition}
)

// AllEvents is the full set the SDK recognizes — the Go mirror of
// contract.AllEvents. Kept so tooling/tests can range over every event name.
var AllEvents = []string{
	EventSpawn, EventTick, EventIdle, EventArrived, EventBlocked,
	EventConstructionStarted, EventResourceDelivered, EventConstructionComplete,
	EventSpotDepleted, EventStorageFull, EventInventoryFull,
	EventRobotProduced, EventRobotDestroyed, EventChargeComplete, EventMessage,
	EventBaseLevelUp, EventQuestUpdated,
	EventResourceProduced, EventProductionBlocked, EventBuildingDestroyed, EventDecommissionStarted,
	EventRobotExpired, EventMaintenanceNeeded, EventBuildingStopped, EventRepairComplete,
}

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
	// CmdRepair (#42): a Mechanic robot on a worn building starts a repair process
	// that drains its held metal into the building's condition. Robot-scoped, no
	// args (targets the building on the robot's cell, like CmdCharge).
	CmdRepair = "repair"
)

// Robot type enum (#42): the build_robot type argument. Distinct classes with
// different stat/lifespan profiles, unlocked at successive Base levels. Mirror of
// the engine's robot classes; "builder" is the default (the starting class).
const (
	RobotBuilder     = "builder"      // L1 start: generalist
	RobotHauler      = "hauler"       // L2: big cargo, slow
	RobotScout       = "scout"        // L2: fast, far, low cargo
	RobotMechanic    = "mechanic"     // L2: repairs wearing buildings
	RobotHeavyHauler = "heavy_hauler" // L4: advanced logistics
	RobotRanger      = "ranger"       // L4: advanced long-lived explorer
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
	StateIdle      = "idle"
	StateMoving    = "moving"
	StateCharging  = "charging"
	StateHauling   = "hauling"
	StateRepairing = "repairing" // #42: a Mechanic running a repair on a worn building
	StateBlocked   = "blocked"
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
