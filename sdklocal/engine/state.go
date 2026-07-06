package engine

import (
	"encoding/json"
	"sort"
)

// --- state.* forms (the JSON the SDK read model consumes) ---

func (m *Module) robotForm(r *robot) map[string]any {
	return map[string]any{
		"id": r.id, "type": r.typ, "pos": []float64{r.pos[0], r.pos[1]},
		"facing":    r.face,
		"inventory": map[string]any{"ore": r.ore, "metal": r.metal, "capacity": r.cap},
		"energy":    r.energy, "state": r.state, "command": r.command(),
	}
}

func (m *Module) buildingForm(b *building) map[string]any {
	w, h := b.w, b.h
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	bf := map[string]any{"id": b.id, "type": b.typ, "pos": []int{b.pos[0], b.pos[1]}, "w": w, "h": h, "status": b.status}
	if b.hasStorage {
		bf["storage"] = map[string]any{"ore": b.ore, "metal": b.metal, "capacity": b.cap}
	}
	if b.typ == BuildingMining {
		if cl := m.wd.cellAt(b.pos[0], b.pos[1]); cl.spot != nil {
			bf["spot"] = map[string]any{"resource": cl.spot.resource, "remaining": cl.spot.remaining}
		}
	}
	if b.typ == BuildingFlyingStation {
		// Robot production lives on the Flying Station (store form comes from
		// hasStorage above). Progress is 0..1 over the recipe's build time.
		denom := m.cfg.RobotRecipe.BuildTicks
		if denom < 1 {
			denom = 1
		}
		bf["production"] = map[string]any{
			"active":   b.prodActive,
			"progress": float64(b.prodProgress) / float64(denom),
			"queued":   b.prodQueue,
		}
	}
	if b.typ == BuildingBase {
		// Leveling: the Base's current level + quest (required vs progress). This
		// is the game objective; only the Base carries it. No production / no
		// general storage form (hasStorage is false).
		lvl := b.level
		if lvl < 1 {
			lvl = 1
		}
		reqOre, reqMetal := m.cfg.questFor(lvl)
		bf["level"] = lvl
		bf["quest"] = map[string]any{
			"required": map[string]any{"ore": reqOre, "metal": reqMetal},
			"progress": map[string]any{"ore": minInt(b.ore, reqOre), "metal": minInt(b.metal, reqMetal)},
		}
	}
	if b.status == StatusConstructing && b.cons != nil {
		bf["construction"] = map[string]any{
			"required":  map[string]any{"ore": b.cons.reqOre, "metal": b.cons.reqMetal},
			"delivered": map[string]any{"ore": b.cons.gotOre, "metal": b.cons.gotMetal},
			"progress":  b.cons.progress,
		}
	}
	return bf
}

func (m *Module) tileForm(x, y int) map[string]any {
	cl := m.wd.cellAt(x, y)
	t := map[string]any{"x": x, "y": y, "terrain": cl.terrain, "spot": nil}
	if cl.spot != nil {
		t["spot"] = map[string]any{"resource": cl.spot.resource, "remaining": cl.spot.remaining}
	}
	return t
}

func (m *Module) statsMap() map[string]any {
	wd := m.wd
	oreStored, metalStored, spots := 0, 0, 0
	for _, b := range wd.buildings {
		oreStored += b.ore
		metalStored += b.metal
	}
	for _, r := range wd.robots {
		oreStored += r.ore
		metalStored += r.metal
	}
	for c := range wd.discovered {
		if cl := wd.cellAt(c[0], c[1]); cl.spot != nil {
			spots++
		}
	}
	return map[string]any{
		"robots":      len(wd.robots),
		"buildings":   len(wd.buildings),
		"ore":         map[string]any{"mined": wd.oreMined, "stored": oreStored},
		"metal":       map[string]any{"mined": wd.metalMined, "stored": metalStored},
		"spots_found": spots,
	}
}

func (m *Module) worldHeader() map[string]any {
	wd := m.wd
	w := map[string]any{"seed": wd.seed, "endless": true, "size": []int{0, 0}, "origin": []int{0, 0}}
	if wd.haveBounds {
		w["origin"] = []int{wd.minX, wd.minY}
		w["size"] = []int{wd.maxX - wd.minX + 1, wd.maxY - wd.minY + 1}
	}
	return w
}

// sortedCells returns the discovered cells ordered (y, then x) for determinism.
func (m *Module) sortedCells() [][2]int {
	out := make([][2]int, 0, len(m.wd.discovered))
	for c := range m.wd.discovered {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i][1] != out[j][1] {
			return out[i][1] < out[j][1]
		}
		return out[i][0] < out[j][0]
	})
	return out
}

func sortedRobotIDs(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

// StateJSON produces the state.* JSON strings the SDK read model consumes, keyed
// by sub-key. The driver picks them in stateKeys order (meta, world, robots,
// buildings, tiles, discovered) for the read-model MGET-equivalent.
func (m *Module) StateJSON(tick, seq int64) map[string]string {
	wd := m.wd
	cells := m.sortedCells()

	tiles := make([]map[string]any, 0, len(cells))
	discovered := make([][2]int, 0, len(cells))
	for _, c := range cells {
		tiles = append(tiles, m.tileForm(c[0], c[1]))
		discovered = append(discovered, c)
	}

	robotIDs := make([]string, 0, len(wd.robots))
	for id := range wd.robots {
		robotIDs = append(robotIDs, id)
	}
	robotIDs = sortedRobotIDs(robotIDs)
	robots := make([]map[string]any, 0, len(robotIDs))
	for _, id := range robotIDs {
		robots = append(robots, m.robotForm(wd.robots[id]))
	}

	buildIDs := make([]string, 0, len(wd.buildings))
	for id := range wd.buildings {
		buildIDs = append(buildIDs, id)
	}
	sort.Strings(buildIDs)
	buildings := make([]map[string]any, 0, len(buildIDs))
	for _, id := range buildIDs {
		buildings = append(buildings, m.buildingForm(wd.buildings[id]))
	}

	meta := map[string]any{"tick": tick, "seq": seq, "city": wd.city}

	marshal := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	return map[string]string{
		"meta":       marshal(meta),
		"world":      marshal(m.worldHeader()),
		"robots":     marshal(robots),
		"buildings":  marshal(buildings),
		"tiles":      marshal(tiles),
		"discovered": marshal(discovered),
		"stats":      marshal(m.statsMap()),
		// Game-agnostic goal summary (Base level + next quest) — the shell topbar.
		"objective": marshal(m.objective()),
	}
}

// SummaryData is the end-of-run scorecard. RobotsDestroyed is filled by the driver.
type SummaryData struct {
	FinalTick       int64          `json:"final_tick"`
	Robots          int            `json:"robots"`
	RobotsDestroyed int            `json:"robots_destroyed"`
	Buildings       int            `json:"buildings"`
	BuildingsByType map[string]int `json:"buildings_by_type"`
	OreMined        int            `json:"-"`
	OreStored       int            `json:"-"`
	MetalMined      int            `json:"-"`
	MetalStored     int            `json:"-"`
	SpotsFound      int            `json:"spots_found"`
	DiscoveredCells int            `json:"discovered_cells"`
}

// Summary computes the scorecard at the given tick (RobotsDestroyed set by caller).
func (m *Module) Summary(tick int64) SummaryData {
	wd := m.wd
	byType := map[string]int{}
	for _, b := range wd.buildings {
		byType[b.typ]++
	}
	oreStored, metalStored, spots := 0, 0, 0
	for _, b := range wd.buildings {
		oreStored += b.ore
		metalStored += b.metal
	}
	for _, r := range wd.robots {
		oreStored += r.ore
		metalStored += r.metal
	}
	for c := range wd.discovered {
		if cl := wd.cellAt(c[0], c[1]); cl.spot != nil {
			spots++
		}
	}
	return SummaryData{
		FinalTick:       tick,
		Robots:          len(wd.robots),
		Buildings:       len(wd.buildings),
		BuildingsByType: byType,
		OreMined:        wd.oreMined,
		OreStored:       oreStored,
		MetalMined:      wd.metalMined,
		MetalStored:     metalStored,
		SpotsFound:      spots,
		DiscoveredCells: len(wd.discovered),
	}
}
