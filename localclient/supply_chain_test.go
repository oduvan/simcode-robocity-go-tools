package simcode

import (
	"encoding/json"
	"testing"
)

// A resource_produced event carries the building-addressed payload {building_id,
// item, amount}; the typed accessors decode each (amount survives the JSON
// float64 round-trip).
func TestResourceProducedEventDecodes(t *testing.T) {
	raw := `{"city":"c1","type":"event","event":"resource_produced","robot":"",` +
		`"tick":42,"payload":{"building_id":"smelter-2","item":"plate","amount":3}}`
	ev, err := decodeEvent([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Event != EventResourceProduced {
		t.Fatalf("event=%q want %q", ev.Event, EventResourceProduced)
	}
	if ev.BuildingID() != "smelter-2" {
		t.Fatalf("BuildingID=%q want smelter-2", ev.BuildingID())
	}
	if ev.Item() != "plate" {
		t.Fatalf("Item=%q want plate", ev.Item())
	}
	if ev.Amount() != 3 {
		t.Fatalf("Amount=%d want 3", ev.Amount())
	}
	// Non-payload accessors default cleanly.
	if ev.Reason() != "" {
		t.Fatalf("Reason=%q want empty", ev.Reason())
	}
}

// production_blocked carries {building_id, reason}.
func TestProductionBlockedEventDecodes(t *testing.T) {
	raw := `{"city":"c1","type":"event","event":"production_blocked","robot":"",` +
		`"payload":{"building_id":"assembler-9","reason":"input_short"}}`
	ev, _ := decodeEvent([]byte(raw))
	if ev.BuildingID() != "assembler-9" || ev.Reason() != "input_short" {
		t.Fatalf("blocked decode wrong: id=%q reason=%q", ev.BuildingID(), ev.Reason())
	}
	if ev.Amount() != 0 || ev.Item() != "" {
		t.Fatalf("absent fields not clean: item=%q amount=%d", ev.Item(), ev.Amount())
	}
}

// A processor building surfaces its input/output pools and its fixed recipe from
// the decoded snapshot.
func TestBuildingProcessorHandles(t *testing.T) {
	vals := []string{
		`{}`, `{}`,
		`[]`, // robots
		`[{"id":"sm1","type":"smelter","pos":[4,4],"status":"active",` +
			`"input":{"items":{"ore":8},"capacity":20},` +
			`"output":{"items":{"plate":2},"capacity":20},` +
			`"recipe":{"inputs":{"ore":2},"output":"plate","out_amount":1,"ticks":5}}]`,
		`[]`, // tiles
		"",
	}
	snap := decodeSnapshot(vals)
	b := &Building{ID: "sm1", city: &City{}, data: snap.buildings["sm1"]}

	if b.Type() != BuildingSmelter {
		t.Fatalf("Type=%q want smelter", b.Type())
	}
	in := b.Input()
	if in == nil || in.Get("ore") != 8 || in.Capacity != 20 {
		t.Fatalf("Input wrong: %+v", in)
	}
	out := b.Output()
	if out == nil || out.Get("plate") != 2 {
		t.Fatalf("Output wrong: %+v", out)
	}
	rec := b.Recipe()
	if rec == nil || rec.Output() != "plate" || rec.OutAmount() != 1 || rec.Ticks() != 5 ||
		rec.Inputs()["ore"] != 2 {
		t.Fatalf("Recipe wrong: %+v", rec)
	}
	// A non-processor building leaves the processor accessors nil (not empty).
	base := &Building{ID: "b0", city: &City{}, data: buildingState{Type: BuildingBase}}
	if base.Input() != nil || base.Output() != nil || base.Recipe() != nil ||
		base.Recoverable() != nil {
		t.Fatal("non-processor should expose nil processor handles")
	}
}

// destroy is world-scoped (args [x, y]); World().Destroy and Building.Destroy
// both target "world" with the coordinates.
func TestDestroyWireArgs(t *testing.T) {
	c := &City{id: "c1", acc: newAccumulator()}
	World{city: c}.Destroy(6, 7)
	intents := c.acc.buildIntents("c1", "")
	b, _ := json.Marshal(intents[0])
	want := `{"city":"c1","type":"intent","robot":"world","commands":[{"cmd":"destroy","args":[6,7]}]}`
	if string(b) != want {
		t.Fatalf("world destroy mismatch\n got: %s\nwant: %s", b, want)
	}

	c2 := &City{id: "c1", acc: newAccumulator()}
	bh := &Building{ID: "sm1", city: c2, data: buildingState{Type: BuildingSmelter, Pos: &[2]int{6, 7}}}
	bh.Destroy()
	i2 := c2.acc.buildIntents("c1", "")
	b2, _ := json.Marshal(i2[0])
	if string(b2) != want {
		t.Fatalf("building destroy mismatch\n got: %s\nwant: %s", b2, want)
	}
}
