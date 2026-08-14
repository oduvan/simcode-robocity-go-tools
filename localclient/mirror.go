// The world mirror: the full world as maps, updated by applying each per-tick delta
// field-wise — a direct port of the Python tool's WorldMirror (simcode/_local.py),
// which itself mirrors the browser reducer. robots/buildings merge by id on their
// nested objects; the map accumulates (discovered RUNS union in, spots upsert by
// cell); `removed` ids drop out (a removed
// robot => destroyed++). The first delta (full-from-empty) establishes the world;
// later ones patch it. The mirror is then projected into the client library's state.* JSON so
// the unchanged decodeSnapshot builds the read model (see driver.publishState).
package simcode

import (
	"encoding/json"
	"sort"
)

type worldMirror struct {
	city string
	seed int64
	tick int64
	seq  int64

	robots     map[string]map[string]any
	buildings  map[string]map[string]any
	spots      map[[2]int][]any // (x,y) -> [x,y,resource,remaining]
	discovered map[[2]int]struct{}
	stats      map[string]any

	// Robots that LEFT the world over the run. A removal alone does not say WHY,
	// and the two reasons are opposites (#73 / forum post 23):
	//   expired   — flew past its lifespan. Inevitable, expected, replace it.
	//   destroyed — battery hit 0 mid-flight. Avoidable; the controller is wrong.
	// The reason rides only on the EVENT, so the driver counts those two and sets
	// these; `removed` is the raw removal count, kept so an unattributable removal
	// can be shown as such instead of being mislabelled as either.
	expired   int
	destroyed int
	removed   int
}

func newWorldMirror(city string, seed int64) *worldMirror {
	return &worldMirror{
		city:       city,
		seed:       seed,
		seq:        -1,
		robots:     map[string]map[string]any{},
		buildings:  map[string]map[string]any{},
		spots:      map[[2]int][]any{},
		discovered: map[[2]int]struct{}{},
		stats:      map[string]any{},
	}
}

// apply merges one `changes` delta into the mirror.
func (m *worldMirror) apply(raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var d map[string]any
	if json.Unmarshal(raw, &d) != nil {
		return
	}

	if v, ok := d["tick"]; ok {
		m.tick = toInt(v)
	}
	if v, ok := d["seq"]; ok {
		m.seq = toInt(v)
	}

	for _, e := range toSlice(d["robots"]) {
		patch, _ := e.(map[string]any)
		id, _ := patch["id"].(string)
		if id == "" {
			continue
		}
		m.robots[id] = mergeRobot(m.robots[id], patch)
	}

	for _, e := range toSlice(d["buildings"]) {
		patch, _ := e.(map[string]any)
		id, _ := patch["id"].(string)
		if id == "" {
			continue
		}
		m.buildings[id] = mergeBuilding(m.buildings[id], patch)
	}

	// Spots are UPSERTS keyed by cell: [x, y, resource, remaining]. remaining 0 means
	// depleted, not gone, so the entry stays.
	for _, e := range toSlice(d["spots"]) {
		sp := toSlice(e)
		if len(sp) != 4 {
			continue
		}
		x, y := int(toInt(sp[0])), int(toInt(sp[1]))
		m.spots[[2]int{x, y}] = sp
	}

	// Discovered arrives as runs to ADD: [y, x0, x1], INCLUSIVE at both ends. This is
	// a union, never a replacement — deltas are incremental.
	for _, e := range toSlice(d["discovered"]) {
		run := toSlice(e)
		if len(run) != 3 {
			continue
		}
		y, x0, x1 := int(toInt(run[0])), int(toInt(run[1])), int(toInt(run[2]))
		for x := x0; x <= x1; x++ {
			m.discovered[[2]int{x, y}] = struct{}{}
		}
	}

	if removed, ok := d["removed"].(map[string]any); ok {
		for _, e := range toSlice(removed["robots"]) {
			if id, _ := e.(string); id != "" {
				if _, existed := m.robots[id]; existed {
					delete(m.robots, id)
					// Count the departure; the REASON comes from the event stream
					// (see the counter note on worldMirror) — a removal on its own
					// cannot tell end-of-life from an energy death.
					m.removed++
				}
			}
		}
		for _, e := range toSlice(removed["buildings"]) {
			if id, _ := e.(string); id != "" {
				delete(m.buildings, id)
			}
		}
	}

	if st, ok := d["stats"].(map[string]any); ok {
		for k, v := range st {
			m.stats[k] = v
		}
	}
}

// mergeRobot merges a robot patch onto its previous state; the nested inventory is
// merged field-wise (matches the browser reducer / Python _merge_robot).
func mergeRobot(prev, patch map[string]any) map[string]any {
	if prev == nil {
		return cloneMap(patch)
	}
	out := cloneMap(prev)
	for k, v := range patch {
		out[k] = v
	}
	if inv, ok := patch["inventory"].(map[string]any); ok {
		out["inventory"] = mergeNested(prev["inventory"], inv)
	}
	return out
}

// mergeBuilding merges a building patch; storage/spot/production/quest merge
// field-wise, and construction merges its nested required/delivered/progress.
func mergeBuilding(prev, patch map[string]any) map[string]any {
	if prev == nil {
		return cloneMap(patch)
	}
	out := cloneMap(prev)
	for k, v := range patch {
		out[k] = v
	}
	for _, field := range []string{"storage", "spot", "production", "quest"} {
		if pv, ok := patch[field].(map[string]any); ok {
			out[field] = mergeNested(prev[field], pv)
		}
	}
	if nc, ok := patch["construction"].(map[string]any); ok {
		pc, _ := prev["construction"].(map[string]any)
		merged := map[string]any{
			"required":  mergeNested(mapField(pc, "required"), mapField(nc, "required")),
			"delivered": mergeNested(mapField(pc, "delivered"), mapField(nc, "delivered")),
		}
		if v, ok := nc["progress"]; ok {
			merged["progress"] = v
		} else if pc != nil {
			merged["progress"] = pc["progress"]
		}
		out["construction"] = merged
	}
	return out
}

// mergeNested shallow-merges a nested object patch onto its previous value.
func mergeNested(prev any, patch map[string]any) map[string]any {
	out := map[string]any{}
	if pm, ok := prev.(map[string]any); ok {
		for k, v := range pm {
			out[k] = v
		}
	}
	for k, v := range patch {
		out[k] = v
	}
	return out
}

func mapField(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, _ := m[key].(map[string]any)
	return v
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func toSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

// toInt coerces a JSON-decoded number (float64) — or an int — to int64.
func toInt(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

// ---- project the mirror into the state.* JSON the read model decodes ----

func (m *worldMirror) metaJSON() string {
	return marshalString(map[string]any{"tick": m.tick, "seq": m.seq, "city": m.city})
}

func (m *worldMirror) worldJSON() string {
	origin := [2]int{0, 0}
	size := [2]int{0, 0}
	if len(m.discovered) > 0 {
		first := true
		var minX, minY, maxX, maxY int
		for c := range m.discovered {
			if first {
				minX, minY, maxX, maxY = c[0], c[1], c[0], c[1]
				first = false
				continue
			}
			if c[0] < minX {
				minX = c[0]
			}
			if c[1] < minY {
				minY = c[1]
			}
			if c[0] > maxX {
				maxX = c[0]
			}
			if c[1] > maxY {
				maxY = c[1]
			}
		}
		origin = [2]int{minX, minY}
		size = [2]int{maxX - minX + 1, maxY - minY + 1}
	}
	return marshalString(map[string]any{
		"seed": m.seed, "size": size, "origin": origin, "endless": true,
	})
}

func (m *worldMirror) robotsJSON() string    { return marshalString(sortedValues(m.robots)) }
func (m *worldMirror) buildingsJSON() string { return marshalString(sortedValues(m.buildings)) }

// spotsJSON re-encodes the sparse deposit list, ordered for determinism.
func (m *worldMirror) spotsJSON() string {
	cells := make([][2]int, 0, len(m.spots))
	for c := range m.spots {
		cells = append(cells, c)
	}
	sort.Slice(cells, func(i, j int) bool {
		if cells[i][1] != cells[j][1] {
			return cells[i][1] < cells[j][1]
		}
		return cells[i][0] < cells[j][0]
	})
	out := make([][]any, 0, len(cells))
	for _, c := range cells {
		out = append(out, m.spots[c])
	}
	return marshalString(out)
}

// discoveredJSON re-encodes the revealed set as per-row inclusive runs [y, x0, x1] —
// the same shape the live wire carries, so the unchanged decodeSnapshot parses it.
func (m *worldMirror) discoveredJSON() string {
	byRow := map[int][]int{}
	for c := range m.discovered {
		byRow[c[1]] = append(byRow[c[1]], c[0])
	}
	rows := make([]int, 0, len(byRow))
	for y := range byRow {
		rows = append(rows, y)
	}
	sort.Ints(rows)
	out := [][3]int{}
	for _, y := range rows {
		xs := byRow[y]
		sort.Ints(xs)
		start, prev := xs[0], xs[0]
		for _, x := range xs[1:] {
			if x == prev+1 {
				prev = x
				continue
			}
			out = append(out, [3]int{y, start, prev})
			start, prev = x, x
		}
		out = append(out, [3]int{y, start, prev})
	}
	return marshalString(out)
}

// sortedValues returns the map values ordered by key (deterministic re-encoding).
func sortedValues(m map[string]map[string]any) []map[string]any {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}

func marshalString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// ---- summary ----

func (m *worldMirror) summary() summaryData {
	byType := map[string]int{}
	baseLevel := 0
	for _, b := range m.buildings {
		typ, _ := b["type"].(string)
		byType[typ]++
		if typ == BuildingBase {
			baseLevel = int(toInt(b["level"]))
		}
	}
	oreMined, oreStored := statPair(m.stats, "ore")
	metalMined, metalStored := statPair(m.stats, "metal")
	spots := int(toInt(m.stats["spots_found"]))

	unattributed := m.removed - m.expired - m.destroyed
	if unattributed < 0 {
		unattributed = 0
	}

	return summaryData{
		FinalTick:          m.tick,
		Robots:             len(m.robots),
		RobotsExpired:      m.expired,
		RobotsDestroyed:    m.destroyed,
		RobotsUnattributed: unattributed,
		Buildings:          len(m.buildings),
		BuildingsByType:    byType,
		OreMined:           oreMined,
		OreStored:          oreStored,
		MetalMined:         metalMined,
		MetalStored:        metalStored,
		SpotsFound:         spots,
		DiscoveredCells:    len(m.discovered),
		BaseLevel:          baseLevel,
	}
}

func statPair(stats map[string]any, key string) (mined, stored int) {
	res, _ := stats[key].(map[string]any)
	if res == nil {
		return 0, 0
	}
	return int(toInt(res["mined"])), int(toInt(res["stored"]))
}
