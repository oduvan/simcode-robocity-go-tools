package simcode

import (
	"encoding/json"
	"io"
	"math"
	"os"
	"strings"
	"testing"
)

// captureStdout runs f with os.Stdout redirected to a pipe and returns what it wrote.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	f()
	_ = w.Close()
	os.Stdout = orig
	return <-done
}

// starterDirs mirrors the shipped starter's compass headings.
var starterDirs = [8][2]int{{1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, -1}}

// registerStarter registers the exact logic of the shipped Go starter
// (templates/go-starter/main.go): explore outward, recharge at the Base before the
// battery runs dry, advancing the heading via per-robot memory each trip. It relies
// on r.Memory()["hop"].(float64), so it also exercises the JSON memory round-trip.
func registerStarter(c *City) {
	c.On(EventIdle, func(e Event) {
		r := c.Robot(e.Robot)
		x, y := r.Position()
		if home := math.Hypot(x, y); r.Energy() < home+15 {
			if cx, cy := r.Cell(); cx == 0 && cy == 0 {
				r.Charge()
			} else {
				r.MoveTo(0, 0)
			}
			return
		}
		n := 0
		if v, ok := r.Memory()["hop"].(float64); ok {
			n = int(v)
		}
		n++
		r.SetMemory(map[string]any{"hop": n})
		d := starterDirs[n%len(starterDirs)]
		r.Log("exploring")
		r.MoveTo(x+float64(d[0]*5), y+float64(d[1]*5))
	})
}

// TestSmokeRealEngine drives the shipped starter against the REAL engine for 120
// ticks on seed 7 and asserts the exact scorecard the PYTHON tool (the reference we
// ported) produces for the SAME starter/seed/ticks — proving the Go local runner and
// the Python one are behaviourally identical (they share the same engine + logic).
// It is gated on $SIMCODE_ENGINE_SO (a local engine build); without it, there is no
// engine to load, so the test skips.
//
// NOTE on the discovered-cell count (1061): this is the value BOTH the Go and Python
// tools produce for the CURRENT canonical starter (math.Hypot home-distance + a
// per-robot memory "hop" heading). An earlier explorer starter — Manhattan
// home-distance + an id-seeded package-level trip counter — swept further and revealed
// 2208 cells; that older controller is what the original gate figure referred to. The
// engine plumbing reproduces 2208 exactly for that controller, so 1061 here is the
// faithful number for THIS starter, not a regression.
func TestSmokeRealEngine(t *testing.T) {
	if os.Getenv("SIMCODE_ENGINE_SO") == "" {
		t.Skip("set SIMCODE_ENGINE_SO to a local engine .so to run the real-engine smoke test")
	}
	t.Setenv("ROBOCITY_SIM_TICKS", "120")
	t.Setenv("ROBOCITY_SIM_SEED", "7")
	t.Setenv("ROBOCITY_SIM_CITY", "local")
	t.Setenv("ROBOCITY_SIM_JSON", "1")

	out := captureStdout(t, func() {
		c := New()
		registerStarter(c)
		_ = c.Run()
	})

	var doc struct {
		Seed    int64 `json:"seed"`
		Ticks   int64 `json:"ticks"`
		Summary struct {
			Robots          int            `json:"robots"`
			RobotsDestroyed int            `json:"robots_destroyed"`
			BuildingsByType map[string]int `json:"buildings_by_type"`
			BaseLevel       int            `json:"base_level"`
			DiscoveredCells int            `json:"discovered_cells"`
		} `json:"summary"`
		Errors []any `json:"errors"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}

	s := doc.Summary
	if s.Robots != 2 {
		t.Errorf("robots = %d, want 2", s.Robots)
	}
	if s.RobotsDestroyed != 0 {
		t.Errorf("robots destroyed = %d, want 0", s.RobotsDestroyed)
	}
	if s.BuildingsByType["base"] != 1 || s.BuildingsByType["storage"] != 1 {
		t.Errorf("buildings = %v, want base=1, storage=1", s.BuildingsByType)
	}
	if s.BaseLevel != 1 {
		t.Errorf("base level = %d, want 1", s.BaseLevel)
	}
	if s.DiscoveredCells != 1061 {
		t.Errorf("discovered cells = %d, want 1061 (matches the Python tool for this starter)", s.DiscoveredCells)
	}
	if len(doc.Errors) != 0 {
		t.Errorf("handler errors = %d, want 0", len(doc.Errors))
	}
}

// TestJSONShapeRealEngine checks the --json document exposes the documented summary
// keys. Also gated on a local engine build.
func TestJSONShapeRealEngine(t *testing.T) {
	if os.Getenv("SIMCODE_ENGINE_SO") == "" {
		t.Skip("set SIMCODE_ENGINE_SO to a local engine .so to run the real-engine smoke test")
	}
	t.Setenv("ROBOCITY_SIM_TICKS", "30")
	t.Setenv("ROBOCITY_SIM_JSON", "1")
	out := captureStdout(t, func() {
		c := New()
		registerStarter(c)
		_ = c.Run()
	})
	var doc struct {
		Summary map[string]any `json:"summary"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	for _, k := range []string{"final_tick", "robots", "robots_destroyed", "buildings",
		"buildings_by_type", "base_level", "ore", "metal", "spots_found", "discovered_cells"} {
		if _, ok := doc.Summary[k]; !ok {
			t.Fatalf("summary missing key %q", k)
		}
	}
	if !strings.Contains(out, "\"seed\"") {
		t.Fatalf("json output missing seed header:\n%s", out)
	}
}
