package engine

import (
	"encoding/json"
	"testing"
)

// TestQuestForSchedule pins the geometric quest escalation (integer math).
func TestQuestForSchedule(t *testing.T) {
	cfg := DefaultConfig()
	cases := []struct{ lvl, ore, metal int }{
		{0, 40, 20}, // < 1 treated as level 1
		{1, 40, 20},
		{2, 60, 30},
		{3, 90, 45},
		{4, 135, 67}, // 90*3/2, 45*3/2 (integer truncation)
	}
	for _, c := range cases {
		if o, m := cfg.questFor(c.lvl); o != c.ore || m != c.metal {
			t.Errorf("questFor(%d) = (%d,%d), want (%d,%d)", c.lvl, o, m, c.ore, c.metal)
		}
	}
}

// TestBaseInitialQuestAnnouncedOnce checks the initial quest_updated fires once.
func TestBaseInitialQuestAnnouncedOnce(t *testing.T) {
	m := New()
	m.ResetWorld("t", 7)
	count := func(evs []Event, name string) int {
		n := 0
		for _, e := range evs {
			if e.Event == name {
				n++
			}
		}
		return n
	}
	if got := count(m.Advance(1), EventQuestUpdated); got != 1 {
		t.Fatalf("initial quest_updated fired %d times, want 1", got)
	}
	if got := count(m.Advance(2), EventQuestUpdated); got != 0 {
		t.Fatalf("quest_updated re-announced with no progress (%d times)", got)
	}
}

// TestBaseLevelsUpAndConsumes checks a delivery clears the quest, consumes the
// store, levels up, and can clear multiple levels in one tick.
func TestBaseLevelsUpAndConsumes(t *testing.T) {
	m := New()
	m.ResetWorld("t", 7)
	b := m.wd.base()
	b.ore, b.metal = 40, 20 // exactly level 1 -> 2
	evs := m.Advance(1)
	if b.level != 2 {
		t.Fatalf("level = %d, want 2", b.level)
	}
	if b.ore != 0 || b.metal != 0 {
		t.Fatalf("store not consumed: ore=%d metal=%d", b.ore, b.metal)
	}
	var lvlups int
	for _, e := range evs {
		if e.Event == EventBaseLevelUp {
			lvlups++
			var p struct {
				Level int            `json:"level"`
				Quest map[string]int `json:"quest"`
			}
			_ = json.Unmarshal(e.Payload, &p)
			if p.Level != 2 || p.Quest["ore"] != 60 || p.Quest["metal"] != 30 {
				t.Fatalf("base_level_up payload = %+v", p)
			}
		}
	}
	if lvlups != 1 {
		t.Fatalf("base_level_up fired %d times, want 1", lvlups)
	}

	// A big surplus clears multiple levels in one tick: L2=60/30, L3=90/45.
	m2 := New()
	m2.ResetWorld("t", 7)
	b2 := m2.wd.base()
	b2.ore, b2.metal = 100, 50 // clears L1 (40/20) then L2 (60/30) -> 0/0
	m2.Advance(1)
	if b2.level != 3 || b2.ore != 0 || b2.metal != 0 {
		t.Fatalf("multi-level: level=%d ore=%d metal=%d, want 3/0/0", b2.level, b2.ore, b2.metal)
	}
}

// TestObjectiveAndBaseForm checks the objective string + Base form carry level/quest.
func TestObjectiveAndBaseForm(t *testing.T) {
	m := New()
	m.ResetWorld("t", 7)
	if got := m.objective(); got != "⭐ Base level 1 — next: 40 ore + 20 metal" {
		t.Fatalf("objective = %q", got)
	}
	bf := m.buildingForm(m.wd.base())
	if bf["level"] != 1 {
		t.Fatalf("base form level = %v, want 1", bf["level"])
	}
	q, ok := bf["quest"].(map[string]any)
	if !ok {
		t.Fatalf("base form quest missing")
	}
	req := q["required"].(map[string]any)
	if req["ore"] != 40 || req["metal"] != 20 {
		t.Fatalf("base form quest.required = %+v", req)
	}
}
