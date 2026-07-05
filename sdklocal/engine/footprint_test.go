package engine

import (
	"encoding/json"
	"testing"
)

// --- multi-cell building footprints (#6) --- mirror of the server engine's
// footprint_test.go.

// A 2×2 Storage occupies all four cells of its footprint; removal clears them.
func TestFootprintOccupiesFourCells(t *testing.T) {
	m := New()
	m.ResetWorld("t", CanonicalSeed)
	st := &building{id: "store-1", typ: BuildingStorage, pos: [2]int{5, 5},
		status: StatusActive, hasStorage: true, cap: 500}
	m.wd.addBuilding(st)

	if st.w != 2 || st.h != 2 {
		t.Fatalf("storage footprint should default to 2×2, got %d×%d", st.w, st.h)
	}
	for _, c := range [][2]int{{5, 5}, {6, 5}, {5, 6}, {6, 6}} {
		if b := m.wd.buildingAt(c[0], c[1]); b == nil || b.id != "store-1" {
			t.Errorf("cell %v should be covered by store-1, got %v", c, b)
		}
	}
	if b := m.wd.buildingAt(7, 5); b != nil {
		t.Errorf("cell (7,5) is outside the footprint, want free, got %q", b.id)
	}

	m.wd.removeBuilding("store-1")
	for _, c := range [][2]int{{5, 5}, {6, 5}, {5, 6}, {6, 6}} {
		if b := m.wd.buildingAt(c[0], c[1]); b != nil {
			t.Errorf("cell %v should be cleared after removal, got %q", c, b.id)
		}
	}
}

// A 1×1 building occupies only its anchor cell (default footprint).
func TestFootprintDefaultSingleCell(t *testing.T) {
	m := New()
	m.ResetWorld("t", CanonicalSeed)
	fs := &building{id: "fs-1", typ: BuildingFlyingStation, pos: [2]int{3, 3}, status: StatusActive}
	m.wd.addBuilding(fs)
	if fs.w != 1 || fs.h != 1 {
		t.Fatalf("flying station should default to 1×1, got %d×%d", fs.w, fs.h)
	}
	for _, c := range [][2]int{{4, 3}, {3, 4}, {4, 4}} {
		if b := m.wd.buildingAt(c[0], c[1]); b != nil {
			t.Errorf("1×1 building must not occupy neighbor %v, got %q", c, b.id)
		}
	}
}

// world.build rejects a placement whose footprint overlaps an existing building.
func TestFootprintPlacementRejectsOverlap(t *testing.T) {
	m := New()
	m.ResetWorld("t", CanonicalSeed)
	m.wd.addBuilding(&building{id: "store-1", typ: BuildingStorage, pos: [2]int{5, 5},
		status: StatusActive, hasStorage: true, cap: 500}) // covers (5,5)-(6,6)
	before := len(m.wd.buildings)

	// A storage anchored at (6,6) covers (6,6)-(7,7), overlapping (6,6).
	evs := m.Submit(Intent{Robot: "", Commands: []Command{{Cmd: CmdBuild, Args: []any{BuildingStorage, 6, 6}}}}, 1)
	blocked := false
	for _, e := range evs {
		if e.Event == EventBlocked {
			var p struct {
				Reason string `json:"reason"`
			}
			if json.Unmarshal(e.Payload, &p) == nil && p.Reason == "cell_occupied" {
				blocked = true
			}
		}
	}
	if !blocked {
		t.Fatalf("overlapping build should be blocked with cell_occupied, got %+v", evs)
	}
	if len(m.wd.buildings) != before {
		t.Errorf("no building should have been placed, have %d want %d", len(m.wd.buildings), before)
	}
}
