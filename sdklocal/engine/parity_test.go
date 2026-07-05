package engine

// Cross-engine PARITY: this Go port must reproduce the golden fixture generated
// from the authoritative server engine (platform repo game/modules/robot_city).
//
// fixtures/parity-seed7.json is a byte-for-byte copy of the platform repo's
// testdata/parity-seed7.json (the real engine's seed-7 starting world + key
// config). If this test fails, the local sim has drifted from the server — a
// change to the Go engine wasn't ported here (see CLAUDE.md). Regenerate the
// fixture on the platform side (PARITY_WRITE=1) and copy it into this repo.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

type pRecipe struct {
	Ore        int `json:"ore"`
	Metal      int `json:"metal"`
	BuildTicks int `json:"build_ticks"`
}

type pConfig struct {
	SpotDensity         float64 `json:"spot_density"`
	SpotRichMin         int     `json:"spot_rich_min"`
	SpotRichMax         int     `json:"spot_rich_max"`
	InitialReveal       int     `json:"initial_reveal"`
	MoveReveal          int     `json:"move_reveal"`
	FlySpeed            float64 `json:"fly_speed"`
	EnergyCap           float64 `json:"energy_cap"`
	EnergyPerDistance   float64 `json:"energy_per_distance"`
	ChargeRate          float64 `json:"charge_rate"`
	CarryCapacity       int     `json:"carry_capacity"`
	NumStartRobots      int     `json:"num_start_robots"`
	StartOre            int     `json:"start_ore"`
	StartMetal          int     `json:"start_metal"`
	ProducedOre         int     `json:"produced_ore"`
	ProducedMetal       int     `json:"produced_metal"`
	MiningSpeed         int     `json:"mining_speed"`
	MiningStorageCap    int     `json:"mining_storage_cap"`
	StorageCap          int     `json:"storage_cap"`
	BaseStorageCap      int     `json:"base_storage_cap"`
	IdleResendTicks     int     `json:"idle_resend_ticks"`
	MiningRecipe        pRecipe `json:"mining_recipe"`
	StorageRecipe       pRecipe `json:"storage_recipe"`
	FlyingStationRecipe pRecipe `json:"flying_station_recipe"`
	RobotRecipe         pRecipe `json:"robot_recipe"`
}

type pSpot struct {
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Resource  string `json:"resource"`
	Remaining int    `json:"remaining"`
}

type pRobot struct {
	ID     string  `json:"id"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Energy float64 `json:"energy"`
	Ore    int     `json:"ore"`
	Metal  int     `json:"metal"`
	Cap    int     `json:"cap"`
}

type pBuilding struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
	W    int    `json:"w"` // footprint width (>=1)
	H    int    `json:"h"` // footprint height (>=1)
	Cap  int    `json:"cap"`
}

type pFixture struct {
	Seed      int64       `json:"seed"`
	Region    int         `json:"region"`
	Config    pConfig     `json:"config"`
	Spots     []pSpot     `json:"spots"`
	Robots    []pRobot    `json:"robots"`
	Buildings []pBuilding `json:"buildings"`
}

func loadParityFixture(t *testing.T) pFixture {
	t.Helper()
	path := filepath.Join("..", "..", "fixtures", "parity-seed7.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read parity fixture %s: %v", path, err)
	}
	var fx pFixture
	if err := json.Unmarshal(data, &fx); err != nil {
		t.Fatalf("parse parity fixture: %v", err)
	}
	return fx
}

func buildParityFromEngine(fx pFixture) pFixture {
	cfg := DefaultConfig()
	wd := newWorld(cfg)
	wd.generate("parity", fx.Seed)

	spots := []pSpot{}
	for x := -fx.Region; x <= fx.Region; x++ {
		for y := -fx.Region; y <= fx.Region; y++ {
			if sp := wd.cellAt(x, y).spot; sp != nil && sp.remaining > 0 {
				spots = append(spots, pSpot{X: x, Y: y, Resource: sp.resource, Remaining: sp.remaining})
			}
		}
	}

	robots := []pRobot{}
	for _, id := range wd.robotOrd {
		r := wd.robots[id]
		robots = append(robots, pRobot{ID: r.id, X: r.pos[0], Y: r.pos[1], Energy: r.energy, Ore: r.ore, Metal: r.metal, Cap: r.cap})
	}
	sort.Slice(robots, func(i, j int) bool { return robots[i].ID < robots[j].ID })

	builds := []pBuilding{}
	for _, id := range wd.buildOrd {
		b := wd.buildings[id]
		builds = append(builds, pBuilding{ID: b.id, Type: b.typ, X: b.pos[0], Y: b.pos[1], W: b.w, H: b.h, Cap: b.cap})
	}
	sort.Slice(builds, func(i, j int) bool { return builds[i].ID < builds[j].ID })

	rec := func(t string) pRecipe {
		r := cfg.Recipes[t]
		return pRecipe{Ore: r.Ore, Metal: r.Metal, BuildTicks: r.BuildTicks}
	}
	return pFixture{
		Seed:   fx.Seed,
		Region: fx.Region,
		Config: pConfig{
			SpotDensity: cfg.SpotDensity, SpotRichMin: cfg.SpotRichMin, SpotRichMax: cfg.SpotRichMax,
			InitialReveal: cfg.InitialReveal, MoveReveal: cfg.MoveReveal,
			FlySpeed: cfg.FlySpeed, EnergyCap: cfg.EnergyCap, EnergyPerDistance: cfg.EnergyPerDistance, ChargeRate: cfg.ChargeRate,
			CarryCapacity: cfg.CarryCapacity, NumStartRobots: cfg.NumStartRobots,
			StartOre: cfg.StartOre, StartMetal: cfg.StartMetal, ProducedOre: cfg.ProducedOre, ProducedMetal: cfg.ProducedMetal,
			MiningSpeed: cfg.MiningSpeed, MiningStorageCap: cfg.MiningStorageCap,
			StorageCap: cfg.StorageCap, BaseStorageCap: cfg.BaseStorageCap, IdleResendTicks: cfg.IdleResendTicks,
			MiningRecipe: rec(BuildingMining), StorageRecipe: rec(BuildingStorage),
			FlyingStationRecipe: rec(BuildingFlyingStation),
			RobotRecipe:         pRecipe{Ore: cfg.RobotRecipe.Ore, Metal: cfg.RobotRecipe.Metal, BuildTicks: cfg.RobotRecipe.BuildTicks},
		},
		Spots: spots, Robots: robots, Buildings: builds,
	}
}

func TestParityAgainstServerFixture(t *testing.T) {
	fx := loadParityFixture(t)
	got := buildParityFromEngine(fx)

	if !reflect.DeepEqual(got.Config, fx.Config) {
		t.Errorf("config drifted from server:\n got=%+v\nwant=%+v", got.Config, fx.Config)
	}
	if !reflect.DeepEqual(got.Spots, fx.Spots) {
		t.Errorf("world-gen spots drifted from server (got %d, want %d)", len(got.Spots), len(fx.Spots))
	}
	if !reflect.DeepEqual(got.Robots, fx.Robots) {
		t.Errorf("initial robots drifted from server:\n got=%+v\nwant=%+v", got.Robots, fx.Robots)
	}
	if !reflect.DeepEqual(got.Buildings, fx.Buildings) {
		t.Errorf("initial buildings drifted from server:\n got=%+v\nwant=%+v", got.Buildings, fx.Buildings)
	}
}
