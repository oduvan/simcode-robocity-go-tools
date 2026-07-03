// Package engine is a standalone, in-process port of the SimCode Robot City
// Builder server engine (game/modules/robot_city in the platform repo). It owns
// the 2D endless world, validates+times robot commands, runs autonomous mining /
// self-completing construction / Base robot production, and emits the full event
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
	StartOre       int
	StartMetal     int
	ProducedOre    int
	ProducedMetal  int

	// Mining (autonomous).
	MiningSpeed      int
	MiningStorageCap int

	// Storage / Base caps.
	StorageCap     int
	BaseStorageCap int

	// Reliability.
	IdleResendTicks int

	// Construction recipes per building type (Base is not buildable).
	Recipes map[string]Recipe

	// Robot production at the Base (consumes the Base's reserved store).
	RobotRecipe Recipe
}

// DefaultConfig returns the provisional v1 tuning values (== Go DefaultConfig()).
func DefaultConfig() Config {
	return Config{
		SpotDensity: 0.05,
		SpotRichMin: 50,
		SpotRichMax: 200,

		InitialReveal: 4,
		MoveReveal:    5,

		FlySpeed:          3,
		EnergyCap:         100,
		EnergyPerDistance: 1,
		ChargeRate:        10,

		CarryCapacity:  10,
		NumStartRobots: 2,
		StartOre:       6,
		StartMetal:     3,
		ProducedOre:    6,
		ProducedMetal:  3,

		MiningSpeed:      1,
		MiningStorageCap: 12,

		StorageCap:     500,
		BaseStorageCap: 200,

		IdleResendTicks: 3,

		Recipes: map[string]Recipe{
			BuildingMining:        {Ore: 6, Metal: 3, BuildTicks: 4},
			BuildingStorage:       {Ore: 3, Metal: 0, BuildTicks: 3},
			BuildingFlyingStation: {Ore: 4, Metal: 2, BuildTicks: 3},
		},
		RobotRecipe: Recipe{Ore: 12, Metal: 6, BuildTicks: 8},
	}
}
