package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

// TestHashCellParity pins hashCell + world generation against reference values
// computed by the DONE Python port (which is verified byte-identical to the Go
// server). A divergence here means the local engine drifted from the server.
func TestHashCellParity(t *testing.T) {
	cases := []struct {
		x, y int
		want uint64
	}{
		{0, 0, 14232521865600346940},
		{3, -4, 10218448438136764270},
		{5, 5, 14045586859002040061},
		{-1, -1, 9072536095413467206},
		{12, -30, 615872452625963958},
		{-100, 100, 16663605834134224690},
	}
	for _, c := range cases {
		if got := hashCell(7, c.x, c.y); got != c.want {
			t.Errorf("hashCell(7,%d,%d) = %d, want %d", c.x, c.y, got, c.want)
		}
	}
}

// TestSeed7SpotField pins the seed-7 spot field in [-20,20] to the Python
// reference (43 spots: 26 ore, 17 metal (fewer, richer — balance change #3)) — proves the same map is generated.
func TestSeed7SpotField(t *testing.T) {
	wd := newWorld(DefaultConfig())
	wd.generate("g", 7)
	spots, ore, metal := 0, 0, 0
	for y := -20; y <= 20; y++ {
		for x := -20; x <= 20; x++ {
			if sp := wd.cellAt(x, y).spot; sp != nil {
				spots++
				if sp.resource == "ore" {
					ore++
				} else {
					metal++
				}
			}
		}
	}
	if spots != 43 || ore != 26 || metal != 17 {
		t.Fatalf("seed-7 spot field = %d spots (ore %d, metal %d), want 43 (26/17)", spots, ore, metal)
	}
}

// TestStartWorld checks the deterministic start: an EMPTY Base (quest hub only)
// at origin, a pre-placed Storage holding the boot capital, and 2 idle robots
// that spawn EMPTY with a full battery.
func TestStartWorld(t *testing.T) {
	m := New()
	m.ResetWorld("t", CanonicalSeed)
	if len(m.wd.robots) != 2 {
		t.Fatalf("want 2 start robots, got %d", len(m.wd.robots))
	}
	b := m.wd.base()
	if b == nil || b.pos != [2]int{0, 0} {
		t.Fatalf("base missing or not at origin: %+v", b)
	}
	// The Base is the quest hub ONLY: no withdrawable store, empty, level 1.
	if b.hasStorage || b.ore != 0 || b.metal != 0 {
		t.Fatalf("base should hold no store: hasStorage=%v ore=%d metal=%d", b.hasStorage, b.ore, b.metal)
	}
	if b.level != 1 {
		t.Fatalf("base level = %d, want 1", b.level)
	}
	// The boot capital lives in a pre-placed 2×2 Storage at anchor (2,0).
	st := m.wd.buildingAt(2, 0)
	if st == nil || st.typ != BuildingStorage || st.pos != [2]int{2, 0} {
		t.Fatalf("pre-placed storage missing at (2,0): %+v", st)
	}
	if st.ore != m.cfg.StartCapitalOre || st.metal != m.cfg.StartCapitalMetal {
		t.Fatalf("start capital wrong: ore=%d metal=%d (want %d/%d)",
			st.ore, st.metal, m.cfg.StartCapitalOre, m.cfg.StartCapitalMetal)
	}
	for _, r := range m.wd.robots {
		if r.energy != 100 || r.ore != 0 || r.metal != 0 {
			t.Fatalf("robot %s should spawn empty: energy=%v ore=%d metal=%d", r.id, r.energy, r.ore, r.metal)
		}
	}
}

// TestFlightEnergyAndDestruction: a robot flying away from any station drains
// EnergyPerDistance*FlySpeed per tick and is destroyed when the battery hits 0.
func TestFlightEnergyAndDestruction(t *testing.T) {
	m := New()
	m.ResetWorld("t", CanonicalSeed)
	// Pick one robot; send it far so it can never make it back.
	var id string
	for _, rid := range m.wd.robotOrd {
		id = rid
		break
	}
	r := m.wd.robots[id]
	startE := r.energy

	m.Submit(Intent{Robot: id, Commands: []Command{{Cmd: CmdMoveTo, Args: []any{1000.0, 0.0}}}}, 1)
	// One advance tick of flight.
	m.Advance(2)
	afterOne := m.wd.robots[id]
	if afterOne == nil {
		t.Fatal("robot destroyed too early")
	}
	drain := startE - afterOne.energy
	wantDrain := m.cfg.FlySpeed * m.cfg.EnergyPerDistance
	if drain != wantDrain {
		t.Fatalf("energy drain per tick = %v, want %v", drain, wantDrain)
	}

	// Keep flying until destroyed; must happen within EnergyCap/drain + slack ticks.
	destroyed := false
	for tk := int64(3); tk < 100; tk++ {
		evs := m.Advance(tk)
		for _, e := range evs {
			if e.Event == EventRobotDestroyed && e.Robot == id {
				destroyed = true
			}
		}
		if _, ok := m.wd.robots[id]; !ok {
			break
		}
	}
	if !destroyed {
		t.Fatal("robot never ran out of energy mid-flight")
	}
	if _, ok := m.wd.robots[id]; ok {
		t.Fatal("destroyed robot still present in world")
	}
}

// TestAutonomousMining: an active Mining building fills its store at
// MiningSpeed/tick, capped at MiningStorageCap, then emits storage_full.
func TestAutonomousMining(t *testing.T) {
	m := New()
	m.ResetWorld("t", CanonicalSeed)
	// Inject an ore spot and an active mine on it.
	cx, cy := 3, 3
	m.wd.cellAt(cx, cy).spot = &spot{resource: "ore", remaining: 1000}
	b := &building{id: "mining-x", typ: BuildingMining, pos: [2]int{cx, cy},
		status: StatusActive, hasStorage: true, cap: m.cfg.MiningStorageCap,
		spotCell: &[2]int{cx, cy}}
	m.wd.addBuilding(b)

	prev := 0
	for tk := int64(1); tk <= int64(m.cfg.MiningStorageCap)+5; tk++ {
		m.Advance(tk)
		got := b.ore
		if got > m.cfg.MiningStorageCap {
			t.Fatalf("mine store %d exceeded cap %d", got, m.cfg.MiningStorageCap)
		}
		if got < prev {
			t.Fatalf("mine store went backwards")
		}
		if got-prev > m.cfg.MiningSpeed {
			t.Fatalf("mine gained %d in one tick, > MiningSpeed %d", got-prev, m.cfg.MiningSpeed)
		}
		prev = got
	}
	if b.ore != m.cfg.MiningStorageCap {
		t.Fatalf("mine did not fill to cap: %d != %d", b.ore, m.cfg.MiningStorageCap)
	}
	if !b.fullEmitted {
		t.Fatal("storage_full was never emitted when the mine capped out")
	}
}

// TestSelfCompletingConstruction: a fulfilled site self-completes over BuildTicks
// (no robot present) and becomes an active building under a new id.
func TestSelfCompletingConstruction(t *testing.T) {
	m := New()
	m.ResetWorld("t", CanonicalSeed)
	recipe := m.cfg.Recipes[BuildingStorage]
	b := &building{id: "plat-9", typ: BuildingStorage, pos: [2]int{4, 0},
		status: StatusConstructing,
		cons: &construction{targetType: BuildingStorage, reqOre: recipe.Ore,
			reqMetal: recipe.Metal, buildTicks: recipe.BuildTicks}}
	// Fulfil it up front.
	b.cons.gotOre = recipe.Ore
	b.cons.gotMetal = recipe.Metal
	m.wd.addBuilding(b)

	completedAt := int64(0)
	for tk := int64(1); tk <= int64(recipe.BuildTicks)+3; tk++ {
		evs := m.Advance(tk)
		for _, e := range evs {
			if e.Event == EventConstructionComplete {
				completedAt = tk
			}
		}
	}
	if completedAt == 0 {
		t.Fatal("construction never completed")
	}
	if completedAt < int64(recipe.BuildTicks) {
		t.Fatalf("construction completed too fast at tick %d (buildTicks=%d)", completedAt, recipe.BuildTicks)
	}
	if _, ok := m.wd.buildings["plat-9"]; ok {
		t.Fatal("finished site should have been removed")
	}
	// A new active storage should exist.
	found := false
	for _, id := range m.wd.buildOrd {
		bb := m.wd.buildings[id]
		if bb.typ == BuildingStorage && bb.status == StatusActive {
			found = true
		}
	}
	if !found {
		t.Fatal("no active storage building after completion")
	}
}

// addStation drops an active Flying Station (with a production store) into the
// world at pos, funded with ore/metal.
func addStation(m *Module, pos [2]int, ore, metal int) *building {
	wd := m.wd
	wd.nextBuild++
	b := &building{
		id: BuildingFlyingStation + "-" + itoa(wd.nextBuild), typ: BuildingFlyingStation,
		pos: pos, status: StatusActive, hasStorage: true, cap: m.cfg.StationStorageCap,
		ore: ore, metal: metal,
	}
	wd.addBuilding(b)
	return b
}

// TestStationProduction: queuing build_robot against a funded Flying Station
// produces a new robot AT the station (empty, full energy) after
// RobotRecipe.BuildTicks ticks, consuming the station's OWN store.
func TestStationProduction(t *testing.T) {
	m := New()
	m.ResetWorld("t", CanonicalSeed)
	st := addStation(m, [2]int{5, 0}, m.cfg.RobotRecipe.Ore, m.cfg.RobotRecipe.Metal)
	before := len(m.wd.robots)

	m.Submit(Intent{Robot: st.id, Commands: []Command{{Cmd: CmdBuildRobot, Args: []any{1}}}}, 1)
	if st.prodQueue != 1 {
		t.Fatalf("station queue = %d, want 1", st.prodQueue)
	}

	var produced string
	for tk := int64(2); tk <= int64(m.cfg.RobotRecipe.BuildTicks)+5; tk++ {
		for _, e := range m.Advance(tk) {
			if e.Event == EventRobotProduced {
				produced = e.Robot
			}
		}
	}
	if produced == "" {
		t.Fatal("station never produced a robot")
	}
	if len(m.wd.robots) != before+1 {
		t.Fatalf("robot count = %d, want %d", len(m.wd.robots), before+1)
	}
	if st.ore != 0 || st.metal != 0 {
		t.Fatalf("station store not consumed: ore=%d metal=%d", st.ore, st.metal)
	}
	nr := m.wd.robots[produced]
	if nr.pos != [2]float64{5, 0} || nr.ore != 0 || nr.metal != 0 || nr.energy != m.cfg.EnergyCap {
		t.Fatalf("produced robot wrong: pos=%v ore=%d metal=%d energy=%v", nr.pos, nr.ore, nr.metal, nr.energy)
	}
}

// TestBuildRobotOnNonStationBlocked: build_robot targeting a non-station (the
// Base) is rejected with `not_a_station` and queues nothing.
func TestBuildRobotOnNonStationBlocked(t *testing.T) {
	m := New()
	m.ResetWorld("t", CanonicalSeed)
	b := m.wd.base()
	evs := m.Submit(Intent{Robot: b.id, Commands: []Command{{Cmd: CmdBuildRobot, Args: []any{1}}}}, 1)
	var blocked bool
	for _, e := range evs {
		if e.Event == EventBlocked {
			var p struct{ Reason string `json:"reason"` }
			_ = json.Unmarshal(e.Payload, &p)
			if p.Reason == "not_a_station" {
				blocked = true
			}
		}
	}
	if !blocked {
		t.Fatal("build_robot on the Base should be blocked not_a_station")
	}
	if b.prodQueue != 0 {
		t.Fatalf("base queue = %d, want 0", b.prodQueue)
	}
}

// TestDropAtBaseCapsAtRequirement: a drop at the Base accepts each resource only
// up to the current quest requirement; the remainder stays in the robot.
func TestDropAtBaseCapsAtRequirement(t *testing.T) {
	m := New()
	m.ResetWorld("t", CanonicalSeed)
	b := m.wd.base() // level 1 -> quest 40/20
	var r *robot
	for _, rr := range m.wd.robots {
		r = rr
		break
	}
	r.pos = [2]float64{0, 0} // stand on the Base
	r.ore, r.metal = 50, 25
	m.Submit(Intent{Robot: r.id, Commands: []Command{{Cmd: CmdDrop}}}, 1)
	if b.ore != 40 || b.metal != 20 {
		t.Fatalf("base accepted %d/%d, want capped 40/20", b.ore, b.metal)
	}
	if r.ore != 10 || r.metal != 5 {
		t.Fatalf("robot remainder %d/%d, want 10/5", r.ore, r.metal)
	}
}

// TestStationStoreDropAndPickupReserved: a drop at a Flying Station feeds its
// production store; pick_up from it is blocked `station_reserved`.
func TestStationStoreDropAndPickupReserved(t *testing.T) {
	m := New()
	m.ResetWorld("t", CanonicalSeed)
	st := addStation(m, [2]int{5, 0}, 0, 0)
	var r *robot
	for _, rr := range m.wd.robots {
		r = rr
		break
	}
	r.pos = [2]float64{5, 0}
	r.ore, r.metal = 6, 3
	m.Submit(Intent{Robot: r.id, Commands: []Command{{Cmd: CmdDrop}}}, 1)
	if st.ore != 6 || st.metal != 3 || r.ore != 0 || r.metal != 0 {
		t.Fatalf("drop-to-station: station=%d/%d robot=%d/%d", st.ore, st.metal, r.ore, r.metal)
	}
	evs := m.Submit(Intent{Robot: r.id, Commands: []Command{{Cmd: CmdPickUp}}}, 2)
	var reserved bool
	for _, e := range evs {
		if e.Event == EventBlocked {
			var p struct{ Reason string `json:"reason"` }
			_ = json.Unmarshal(e.Payload, &p)
			if p.Reason == "station_reserved" {
				reserved = true
			}
		}
	}
	if !reserved {
		t.Fatal("pick_up from a station should be blocked station_reserved")
	}
	if st.ore != 6 || st.metal != 3 {
		t.Fatalf("station store changed on blocked pick_up: %d/%d", st.ore, st.metal)
	}
}

// TestDeterministicEngine: the same scripted commands produce byte-identical
// events + final state across two independent runs.
func TestDeterministicEngine(t *testing.T) {
	run := func() string {
		m := New()
		m.ResetWorld("t", CanonicalSeed)
		h := sha256.New()
		enc := json.NewEncoder(h)
		var pending []Intent
		for tk := int64(1); tk <= 80; tk++ {
			var evs []Event
			for _, it := range pending {
				evs = append(evs, m.Submit(it, tk)...)
			}
			pending = nil
			evs = append(evs, m.Advance(tk)...)
			for _, e := range evs {
				_ = enc.Encode(e)
				// react: on idle, fly each robot outward deterministically
				if e.Event == EventIdle {
					r := m.wd.robots[e.Robot]
					if r != nil {
						pending = append(pending, Intent{Robot: e.Robot,
							Commands: []Command{{Cmd: CmdMoveTo, Args: []any{r.pos[0] + 5, r.pos[1]}}}})
					}
				}
			}
			for _, f := range m.DrainFeed() {
				_, _ = h.Write([]byte(f.Line()))
			}
		}
		_ = enc.Encode(m.StateJSON(80, 80))
		return hex.EncodeToString(h.Sum(nil))
	}
	a, b := run(), run()
	if a != b {
		t.Fatalf("engine not deterministic: %s != %s", a, b)
	}
}
