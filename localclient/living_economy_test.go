package simcode

import (
	"encoding/json"
	"testing"
)

// The living-economy overhaul (#42): robot expiry, building maintenance, the
// repair command, and the type-carrying build_robot. These tests pin the wire
// shapes the engine (game/core/contract) freezes.

// repair_complete carries {building_id, robot_id, condition}; the typed accessors
// decode each (condition survives the JSON float64 round-trip).
func TestRepairCompleteEventDecodes(t *testing.T) {
	raw := `{"city":"c1","type":"event","event":"repair_complete","robot":"",` +
		`"tick":88,"payload":{"building_id":"assembler-9","robot_id":"r7","condition":80}}`
	ev, err := decodeEvent([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Event != EventRepairComplete {
		t.Fatalf("event=%q want %q", ev.Event, EventRepairComplete)
	}
	if ev.BuildingID() != "assembler-9" {
		t.Fatalf("BuildingID=%q want assembler-9", ev.BuildingID())
	}
	if ev.RobotID() != "r7" {
		t.Fatalf("RobotID=%q want r7", ev.RobotID())
	}
	if ev.Condition() != 80 {
		t.Fatalf("Condition=%d want 80", ev.Condition())
	}
}

// maintenance_needed carries {building_id, condition}.
func TestMaintenanceNeededEventDecodes(t *testing.T) {
	raw := `{"city":"c1","type":"event","event":"maintenance_needed","robot":"",` +
		`"payload":{"building_id":"alloy_furnace-2","condition":40}}`
	ev, _ := decodeEvent([]byte(raw))
	if ev.Event != EventMaintenanceNeeded {
		t.Fatalf("event=%q want %q", ev.Event, EventMaintenanceNeeded)
	}
	if ev.BuildingID() != "alloy_furnace-2" || ev.Condition() != 40 {
		t.Fatalf("decode wrong: id=%q condition=%d", ev.BuildingID(), ev.Condition())
	}
}

// building_stopped carries just {building_id}; robot_expired carries {robot_id}.
func TestBuildingStoppedAndRobotExpiredDecode(t *testing.T) {
	stopped, _ := decodeEvent([]byte(`{"type":"event","event":"building_stopped",` +
		`"payload":{"building_id":"frame_shop-1"}}`))
	if stopped.Event != EventBuildingStopped || stopped.BuildingID() != "frame_shop-1" {
		t.Fatalf("building_stopped decode wrong: %+v", stopped)
	}
	expired, _ := decodeEvent([]byte(`{"type":"event","event":"robot_expired",` +
		`"payload":{"robot_id":"r3"}}`))
	if expired.Event != EventRobotExpired || expired.RobotID() != "r3" {
		t.Fatalf("robot_expired decode wrong: %+v", expired)
	}
}

// Repair emits a no-arg, robot-scoped `repair` command (mirrors Charge).
func TestRepairEmitsNoArgCommand(t *testing.T) {
	c := &City{id: "c1", acc: newAccumulator()}
	r := &Robot{ID: "r1", city: c}

	r.Repair()

	intents := c.acc.buildIntents("c1", "r1")
	if len(intents) != 1 {
		t.Fatalf("want 1 intent, got %d", len(intents))
	}
	b, _ := json.Marshal(intents[0])
	want := `{"city":"c1","type":"intent","robot":"r1","commands":[{"cmd":"repair","args":[]}]}`
	if string(b) != want {
		t.Fatalf("repair mismatch\n got: %s\nwant: %s", b, want)
	}
}

// A robot surfaces its type + remaining/max lifespan from the decoded snapshot.
func TestRobotLifeAndTypeHandles(t *testing.T) {
	vals := []string{
		`{}`, `{}`,
		`[{"id":"r1","type":"scout","pos":[3,4],"energy":50,` +
			`"life_remaining":620.5,"life_max":800}]`,
		`[]`, `[]`, "",
	}
	snap := decodeSnapshot(vals)
	r := &Robot{ID: "r1", city: &City{}, snap: snap, data: snap.robots["r1"]}

	if r.Type() != RobotScout {
		t.Fatalf("Type=%q want scout", r.Type())
	}
	if r.LifeRemaining() != 620.5 {
		t.Fatalf("LifeRemaining=%v want 620.5", r.LifeRemaining())
	}
	if r.LifeMax() != 800 {
		t.Fatalf("LifeMax=%v want 800", r.LifeMax())
	}
	// Absent lifespan on another robot reads a clean 0.
	empty := &Robot{ID: "x", city: &City{}, data: robotState{}}
	if empty.LifeRemaining() != 0 || empty.LifeMax() != 0 {
		t.Fatalf("absent lifespan not 0: rem=%v max=%v", empty.LifeRemaining(), empty.LifeMax())
	}
}

// A wearing T2/T3 processor surfaces its Condition; the Base surfaces Unlocks.
// A building without either reads nil (never-wears / non-Base).
func TestBuildingConditionAndUnlocks(t *testing.T) {
	vals := []string{
		`{}`, `{}`,
		`[]`,
		`[{"id":"af1","type":"alloy_furnace","pos":[6,6],"status":"active","condition":45},` +
			`{"id":"base","type":"base","pos":[0,0],"level":2,` +
			`"unlocks":["hauler","scout","mechanic","assembler"]},` +
			`{"id":"m1","type":"mining","pos":[2,2],"status":"active"}]`,
		`[]`, "",
	}
	snap := decodeSnapshot(vals)

	af := &Building{ID: "af1", city: &City{}, data: snap.buildings["af1"]}
	if cond := af.Condition(); cond == nil || *cond != 45 {
		t.Fatalf("Condition wrong: %v", cond)
	}

	base := &Building{ID: "base", city: &City{}, data: snap.buildings["base"]}
	un := base.Unlocks()
	if len(un) != 4 || un[0] != "hauler" || un[3] != "assembler" {
		t.Fatalf("Unlocks wrong: %v", un)
	}

	// A building that never wears and isn't the Base: both nil.
	mine := &Building{ID: "m1", city: &City{}, data: snap.buildings["m1"]}
	if mine.Condition() != nil {
		t.Fatalf("mining Condition should be nil, got %v", mine.Condition())
	}
	if mine.Unlocks() != nil {
		t.Fatalf("mining Unlocks should be nil, got %v", mine.Unlocks())
	}
}

// Robots are built at a Flying Station and BuildRobot carries the robot TYPE +
// count (#42), wire args [type, n].
func TestStationBuildRobotTargetsStationID(t *testing.T) {
	c := &City{id: "c1", acc: newAccumulator()}
	st := &Building{ID: "flying_station-3", city: c, data: buildingState{Type: BuildingFlyingStation}}

	st.BuildRobot(RobotHauler, 2)

	intents := c.acc.buildIntents("c1", "")
	if len(intents) != 1 {
		t.Fatalf("want 1 intent, got %d", len(intents))
	}
	b, _ := json.Marshal(intents[0])
	want := `{"city":"c1","type":"intent","robot":"flying_station-3","commands":[{"cmd":"build_robot","args":["hauler",2]}]}`
	if string(b) != want {
		t.Fatalf("station build_robot mismatch\n got: %s\nwant: %s", b, want)
	}
}

// An empty type defaults to builder and a non-positive count clamps to 1 (#42).
func TestStationBuildRobotDefaults(t *testing.T) {
	c := &City{id: "c1", acc: newAccumulator()}
	st := &Building{ID: "flying_station-3", city: c, data: buildingState{Type: BuildingFlyingStation}}

	st.BuildRobot("", 0)

	intents := c.acc.buildIntents("c1", "")
	b, _ := json.Marshal(intents[0])
	want := `{"city":"c1","type":"intent","robot":"flying_station-3","commands":[{"cmd":"build_robot","args":["builder",1]}]}`
	if string(b) != want {
		t.Fatalf("station build_robot defaults mismatch\n got: %s\nwant: %s", b, want)
	}
}
