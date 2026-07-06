// Package engine is a standalone, in-process port of the SimCode Robot City
// Builder server engine (game/modules/robot_city in the platform repo). It owns
// the 2D endless world, validates+times robot commands, runs autonomous mining /
// self-completing construction / Flying Station robot production, and emits the full event
// set + the state.* snapshot the SDK read model consumes.
//
// It is DECOUPLED from the platform: no Redis, no ports, no
// github.com/lyabah/simcode import. A driver calls ResetWorld, then per tick
// Submit(intent) + Advance() and reads StateJSON()/DrainFeed(). Determinism is
// preserved exactly from the Go source: hashCell world-gen, robotOrd/buildOrd
// slices for ordered logic, sorted snapshot collections — same seed → same run.
package engine

// CanonicalSeed is the module's fixed world seed: every city of this type shares
// one map (game/cmd/game/main.go: canonicalSeed = 7).
const CanonicalSeed int64 = 7

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

// Recipe is a building/robot construction cost + build time (in ticks).
type Recipe struct {
	Ore        int
	Metal      int
	BuildTicks int // work units; a fulfilled site self-completes over BuildTicks ticks
}

// Footprint is a building type's rectangular size in cells (W wide, H tall). A
// building's anchor (pos) is the MIN corner; it occupies every cell in
// [x, x+W) × [y, y+H). A robot on ANY covered cell can interact with it.
type Footprint struct{ W, H int }

// Config holds every tunable number for the module. Ported verbatim from
// game/modules/robot_city/config.go DefaultConfig — same numbers → same run.
type Config struct {
	// World generation (endless: generated lazily as discovered).
	SpotDensity float64
	SpotRichMin int
	SpotRichMax int

	// Fog of war.
	InitialReveal int
	MoveReveal    int

	// Flight & energy (energy is spent ONLY on flying).
	FlySpeed          float64
	EnergyCap         float64
	EnergyPerDistance float64
	ChargeRate        float64

	// Robots.
	CarryCapacity  int
	NumStartRobots int
	StartOre       int // each starting robot's inventory kit (0 = spawn empty)
	StartMetal     int
	// ProducedOre/ProducedMetal are RETAINED but currently UNUSED: a
	// station-produced robot now spawns EMPTY. Kept so the kit can be
	// reintroduced without a config/parity-schema change.
	ProducedOre   int
	ProducedMetal int

	// Starting capital: the boot stock lives in a pre-placed Storage next to the
	// Base at world start (the Base itself no longer seeds a store).
	StartCapitalOre   int
	StartCapitalMetal int

	// Mining (autonomous).
	MiningSpeed      int
	MiningStorageCap int

	// Storage caps.
	StorageCap        int
	StationStorageCap int // a Flying Station's robot-production store cap
	// BaseStorageCap is RETAINED but currently UNUSED: the Base's store is the
	// quest accumulator, capped PER-RESOURCE at questFor(level), not by this value.
	BaseStorageCap int

	// Reliability.
	IdleResendTicks int

	// Base quests (the game objective). The Base starts at level 1; each level
	// poses a quest = a required amount of raw ore+metal that must accumulate in
	// the Base's quest store (drops are capped per-resource at the requirement).
	// When both are met, the store RESETS to 0 and the Base levels up to the
	// next, harder quest. questFor(level) escalates the requirement
	// geometrically from the base amounts by QuestGrowthNum/QuestGrowthDen per
	// level. (Mirror of config.go.)
	QuestBaseOre   int
	QuestBaseMetal int
	QuestGrowthNum int
	QuestGrowthDen int

	// Construction recipes per building type (Base is not buildable).
	Recipes map[string]Recipe

	// Footprints per building type. Any type not listed defaults to 1×1 (see
	// footprint). Storage is a 2×2 hub; base/mining/flying_station stay 1×1.
	Footprints map[string]Footprint

	// Robot production at a Flying Station (consumes that station's own store).
	RobotRecipe Recipe
}

// footprint returns the W×H cell footprint for a building type, defaulting to
// 1×1 for any type not explicitly configured.
func (c Config) footprint(typ string) (w, h int) {
	if f, ok := c.Footprints[typ]; ok && f.W > 0 && f.H > 0 {
		return f.W, f.H
	}
	return 1, 1
}

// questFor returns the ore+metal the Base must accumulate to clear the quest at
// the given level (level 1 = the base amounts, each subsequent level scaled by
// QuestGrowthNum/QuestGrowthDen). Pure + deterministic integer math, so it
// reproduces the server engine exactly. Level < 1 is treated as 1.
func (c Config) questFor(level int) (ore, metal int) {
	if level < 1 {
		level = 1
	}
	ore, metal = c.QuestBaseOre, c.QuestBaseMetal
	num, den := c.QuestGrowthNum, c.QuestGrowthDen
	if num <= 0 || den <= 0 {
		return ore, metal
	}
	for i := 1; i < level; i++ {
		ore = ore * num / den
		metal = metal * num / den
	}
	return ore, metal
}

// DefaultConfig returns the provisional v1 tuning values (== Go DefaultConfig()).
func DefaultConfig() Config {
	return Config{
		SpotDensity: 0.025,
		SpotRichMin: 150,
		SpotRichMax: 600,

		InitialReveal: 4,
		MoveReveal:    5,

		FlySpeed:          3,
		EnergyCap:         100,
		EnergyPerDistance: 1,
		ChargeRate:        10,

		CarryCapacity:  10,
		NumStartRobots: 2,
		StartOre:       0, // robots spawn EMPTY — the boot stock lives in a Storage now
		StartMetal:     0,
		ProducedOre:    6, // retained, currently unused (produced robots spawn empty)
		ProducedMetal:  3,
		// Boot capital: a Storage pre-placed next to the Base holds this at start.
		StartCapitalOre:   30,
		StartCapitalMetal: 15,

		MiningSpeed:      1,
		MiningStorageCap: 12,

		StorageCap:        500,
		StationStorageCap: 200, // a Flying Station's production store (~16 robots)
		BaseStorageCap:    200, // retained, currently unused

		IdleResendTicks: 3,

		QuestBaseOre:   40,
		QuestBaseMetal: 20,
		QuestGrowthNum: 3,
		QuestGrowthDen: 2,

		Recipes: map[string]Recipe{
			BuildingMining:        {Ore: 6, Metal: 3, BuildTicks: 4},
			BuildingStorage:       {Ore: 3, Metal: 0, BuildTicks: 3},
			BuildingFlyingStation: {Ore: 4, Metal: 2, BuildTicks: 3},
		},
		Footprints: map[string]Footprint{
			BuildingStorage: {W: 2, H: 2},
		},
		RobotRecipe: Recipe{Ore: 12, Metal: 6, BuildTicks: 8},
	}
}
