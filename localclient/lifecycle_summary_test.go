package simcode

import (
	"encoding/json"
	"testing"
)

// #73 req 4: end-of-life is not failure.
//
// Forum post 23: a long run reported "robots destroyed : 113" when all 113 were
// robot_expired — normal fleet turnover. The two are documented as different
// things with different causes, and only one means the controller is wrong.

func TestSummarySplitsExpiryFromEnergyDeath(t *testing.T) {
	m := newWorldMirror("t", 7)
	m.apply(json.RawMessage(`{"tick":1,"seq":1,"robots":[
		{"id":"r1"},{"id":"r2"},{"id":"r3"},{"id":"r4"}]}`))

	// The driver attributes each departure from the event stream.
	m.expired += 3
	m.destroyed++
	m.apply(json.RawMessage(`{"tick":2,"seq":2,"removed":{"robots":["r1","r2","r3","r4"]}}`))

	s := m.summary()
	if s.RobotsExpired != 3 {
		t.Fatalf("robots expired = %d, want 3", s.RobotsExpired)
	}
	if s.RobotsDestroyed != 1 {
		t.Fatalf("robots destroyed = %d, want 1 (only the energy death)", s.RobotsDestroyed)
	}
	if s.RobotsUnattributed != 0 {
		t.Fatalf("every removal was attributable, got %d unattributed", s.RobotsUnattributed)
	}
}

func TestSummaryReportsAnUnattributableRemoval(t *testing.T) {
	m := newWorldMirror("t", 7)
	m.apply(json.RawMessage(`{"tick":1,"seq":1,"robots":[{"id":"r1"}]}`))
	m.apply(json.RawMessage(`{"tick":2,"seq":2,"removed":{"robots":["r1"]}}`))

	s := m.summary()
	if s.RobotsExpired != 0 || s.RobotsDestroyed != 0 {
		t.Fatalf("an unexplained removal must not be labelled: %+v", s)
	}
	if s.RobotsUnattributed != 1 {
		t.Fatalf("want 1 unattributed removal, got %d", s.RobotsUnattributed)
	}
}

// The split must not depend on the controller subscribing to anything.
func TestLifecycleEventsAreAlwaysRequested(t *testing.T) {
	got := withLifecycleEvents([]string{"idle"})
	want := map[string]bool{"idle": true, "robot_expired": true, "robot_destroyed": true}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for _, ev := range got {
		if !want[ev] {
			t.Fatalf("unexpected %q in %v", ev, got)
		}
		delete(want, ev)
	}
	if len(want) != 0 {
		t.Fatalf("missing %v", want)
	}
	// Idempotent: asking twice must not duplicate.
	if again := withLifecycleEvents(got); len(again) != len(got) {
		t.Fatalf("duplicated: %v", again)
	}
}

func TestDescribeWorldNamesSeedAndOrigin(t *testing.T) {
	c := New()
	c.seed = 100057250
	c.worldOrigin = "city 'my-city' on https://example.test"
	c.cityConfig = map[string]any{"starting_fleet": 5}

	got := c.describeWorld()
	for _, want := range []string{"seed 100057250", "my-city", "starting_fleet"} {
		if !contains(got, want) {
			t.Fatalf("describeWorld() = %q, missing %q", got, want)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
