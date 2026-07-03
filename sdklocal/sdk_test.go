package simcode

import (
	"encoding/json"
	"io"
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

// starterLike is a minimal @idle controller (fly out, charge when low) used to
// exercise the full data-in / intents-out loop through the engine.
func starterLike(c *City) {
	c.On(EventIdle, func(e Event) {
		r := c.Robot(e.Robot)
		base := c.Base()
		if base == nil {
			return
		}
		bx, by := base.Position()
		x, y := r.Position()
		cx, cy := r.Cell()
		home := absf(x-float64(bx)) + absf(y-float64(by))
		atBase := cx == bx && cy == by
		if r.Energy() <= home+15 {
			if atBase {
				r.Charge()
			} else {
				r.MoveTo(float64(bx), float64(by))
			}
			return
		}
		r.Log("exploring")
		r.MoveTo(x+5, y)
	})
}

func absf(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func TestRunProducesSummary(t *testing.T) {
	t.Setenv("ROBOCITY_SIM_TICKS", "200")
	t.Setenv("ROBOCITY_SIM_CITY", "unit")
	out := captureStdout(t, func() {
		c := New()
		starterLike(c)
		if err := c.Run(); err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	})
	if !strings.Contains(out, "SUMMARY") {
		t.Fatalf("output missing SUMMARY:\n%s", out)
	}
	if !strings.Contains(out, "robots            : 2") {
		t.Fatalf("expected 2 robots in summary:\n%s", out)
	}
	if !strings.Contains(out, "robots destroyed  : 0") {
		t.Fatalf("expected 0 destroyed:\n%s", out)
	}
	if !strings.Contains(out, "exploring") {
		t.Fatalf("expected user log lines in the feed:\n%s", out)
	}
}

func TestRunDeterministic(t *testing.T) {
	t.Setenv("ROBOCITY_SIM_TICKS", "150")
	once := func() string {
		return captureStdout(t, func() {
			c := New()
			starterLike(c)
			_ = c.Run()
		})
	}
	a, b := once(), once()
	if a != b {
		t.Fatalf("Run not deterministic across two runs")
	}
}

func TestRunJSONShape(t *testing.T) {
	t.Setenv("ROBOCITY_SIM_TICKS", "120")
	t.Setenv("ROBOCITY_SIM_JSON", "1")
	out := captureStdout(t, func() {
		c := New()
		starterLike(c)
		_ = c.Run()
	})
	var doc struct {
		Seed    int64 `json:"seed"`
		Ticks   int64 `json:"ticks"`
		City    string
		Summary map[string]any `json:"summary"`
		Feed    []struct {
			Tick int64  `json:"tick"`
			Line string `json:"line"`
		} `json:"feed"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if doc.Seed != 7 || doc.Ticks != 120 {
		t.Fatalf("bad header: seed=%d ticks=%d", doc.Seed, doc.Ticks)
	}
	for _, k := range []string{"final_tick", "robots", "robots_destroyed", "buildings",
		"buildings_by_type", "ore", "metal", "spots_found", "discovered_cells"} {
		if _, ok := doc.Summary[k]; !ok {
			t.Fatalf("summary missing key %q", k)
		}
	}
	if len(doc.Feed) == 0 {
		t.Fatal("feed is empty")
	}
}
