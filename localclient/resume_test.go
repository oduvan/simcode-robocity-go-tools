package simcode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #73 req 1 (client half): the resume bundle the CLI writes must reach the driver
// as an engine map state, a seeded store and a seeded read model — all three.

func writeBundle(t *testing.T, doc string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "resume.json")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadResumeBundleSeedsWorldStoreAndReadModel(t *testing.T) {
	path := writeBundle(t, `{"map":{"tick":14460,"world":{"seed":1}},
		"store":{"claims":{"r1":"mining-4"},"version":"abc"},
		"prime":{"tick":14562,"robots":[{"id":"r1"}]},
		"save_tick":14460}`)

	c := &City{handlers: map[string][]Handler{}, storeState: map[string]any{}}
	c.loadResumeBundle(path)

	if !c.resumed || c.saveTick != 14460 {
		t.Fatalf("not resumed: resumed=%v tick=%d", c.resumed, c.saveTick)
	}
	if len(c.mapState) == 0 || len(c.prime) == 0 {
		t.Fatal("map state and read-model seed must both be loaded")
	}
	// Saved values survive a deploy; losing them gives a city that looks right
	// and behaves wrong.
	if len(c.initialStore) != 2 || c.initialStore["version"] != "abc" {
		t.Fatalf("store not seeded: %v", c.initialStore)
	}
}

func TestLoadResumeBundleIsANoOpWithoutAPath(t *testing.T) {
	c := &City{handlers: map[string][]Handler{}, storeState: map[string]any{}}
	c.loadResumeBundle("")
	if c.resumed || c.mapState != nil {
		t.Fatal("no bundle ⇒ a plain cold start")
	}
}

// The resumed world reaches the READ MODEL, not just the engine: priming the
// mirror with the display snapshot is what the handlers actually see.
func TestPrimingTheMirrorPopulatesTheReadModel(t *testing.T) {
	m := newWorldMirror("c", 1)
	m.apply(json.RawMessage(`{"tick":14562,"seq":14562,
		"robots":[{"id":"r1","pos":[1,2],"state":"moving"},{"id":"r2","pos":[3,4],"state":"idle"}],
		"buildings":[{"id":"base-1","type":"base","level":14}],
		"spots":[[1,1,"ore",50]],
		"discovered":[[0,0,3]],
		"stats":{"robots":2,"buildings":1}}`))

	if len(m.robots) != 2 || len(m.buildings) != 1 {
		t.Fatalf("read model not primed: %d robots, %d buildings", len(m.robots), len(m.buildings))
	}
	if len(m.discovered) != 4 {
		t.Fatalf("discovered runs not expanded: %d", len(m.discovered))
	}
	s := m.summary()
	if s.BaseLevel != 14 {
		t.Fatalf("base level = %d, want the resumed city's 14", s.BaseLevel)
	}
}

// The engine is authoritative. When the seeded display state disagrees with it,
// say so rather than pass it off as fact.
func TestReadModelDriftIsReportedOnlyWhenItExists(t *testing.T) {
	c := &City{handlers: map[string][]Handler{}, storeState: map[string]any{}, resumed: true}

	agree := summaryData{Robots: 54, Buildings: 197, EngineRobots: 54, EngineBuildings: 197}
	if got := c.readModelDrift(agree); got != "" {
		t.Fatalf("no drift expected, got %q", got)
	}

	differ := summaryData{Robots: 57, Buildings: 197, EngineRobots: 54, EngineBuildings: 195}
	got := c.readModelDrift(differ)
	if !strings.Contains(got, "57 robots") || !strings.Contains(got, "54 / 195") {
		t.Fatalf("drift should name both views, got %q", got)
	}

	cold := &City{handlers: map[string][]Handler{}, storeState: map[string]any{}}
	if got := cold.readModelDrift(differ); got != "" {
		t.Fatalf("a cold start has no read-model drift, got %q", got)
	}
}

func TestDescribeWorldNamesAResumedRun(t *testing.T) {
	c := &City{handlers: map[string][]Handler{}, storeState: map[string]any{},
		seed: 100057250, resumed: true, saveTick: 14460,
		worldOrigin:  "city 'my-city' on https://example.test, AS IT IS NOW (--from-live)",
		worldStart:   "resumed at tick 14461 (saved world from tick 14460)",
		initialStore: map[string]any{"a": 1, "b": 2}}

	got := c.describeWorld()
	for _, want := range []string{"seed 100057250", "AS IT IS NOW", "resumed at tick 14461",
		"2 saved store key(s) restored"} {
		if !strings.Contains(got, want) {
			t.Fatalf("describeWorld() = %q, missing %q", got, want)
		}
	}
}
