// The world mirror: the full world as maps, updated by applying each per-tick delta
// field-wise — a direct port of the Python tool's WorldMirror (simcode/_local.py),
// which itself mirrors the browser reducer. robots/buildings merge by id on their
// nested objects; tiles/discovered accumulate; `removed` ids drop out (a removed
// robot => destroyed++). The first delta (full-from-empty) establishes the world;
// later ones patch it. The mirror is then projected into the SDK's state.* JSON so
// the unchanged decodeSnapshot builds the read model (see driver.publishState).
package simcode

import (
	"encoding/json"
	"fmt"
	"sort"
)

type worldMirror struct {
	city string
	seed int64
	tick int64
	seq  int64

	robots    map[string]map[string]any
	buildings map[string]map[string]any
	tiles     map[string]map[string]any // "x,y" -> tile
	discovered map[[2]int]struct{}
	stats     map[string]any

	destroyed int
}

func newWorldMirror(city string, seed int64) *worldMirror {
	return &worldMirror{
		city:       city,
		seed:       seed,
		seq:        -1,
		robots:     map[string]map[string]any{},
		buildings:  map[string]map[string]any{},
		tiles:      map[string]map[string]any{},
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

	for _, e := range toSlice(d["tiles"]) {
		t, _ := e.(map[string]any)
		x, okx := t["x"]
		y, oky := t["y"]
		if !okx || !oky {
			continue
		}
		ix, iy := int(toInt(x)), int(toInt(y))
		m.tiles[fmt.Sprintf("%d,%d", ix, iy)] = t
		m.discovered[[2]int{ix, iy}] = struct{}{}
	}

	for _, e := range toSlice(d["discovered"]) {
		xy := toSlice(e)
		if len(xy) < 2 {
			continue
		}
		m.discovered[[2]int{int(toInt(xy[0])), int(toInt(xy[1]))}] = struct{}{}
	}

	if removed, ok := d["removed"].(map[string]any); ok {
		for _, e := range toSlice(removed["robots"]) {
			if id, _ := e.(string); id != "" {
				if _, existed := m.robots[id]; existed {
					delete(m.robots, id)
					// In this module a robot only leaves the world by being destroyed
					// (out of energy mid-flight), so a removed robot id is a faithful
					// destroyed signal, independent of subscriptions.
					m.destroyed++
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

func (m *worldMirror) robotsJSON() string  { return marshalString(sortedValues(m.robots)) }
func (m *worldMirror) buildingsJSON() string { return marshalString(sortedValues(m.buildings)) }
func (m *worldMirror) tilesJSON() string    { return marshalString(sortedValues(m.tiles)) }

func (m *worldMirror) discoveredJSON() string {
	cells := make([][2]int, 0, len(m.discovered))
	for c := range m.discovered {
		cells = append(cells, c)
	}
	sort.Slice(cells, func(i, j int) bool {
		if cells[i][0] != cells[j][0] {
			return cells[i][0] < cells[j][0]
		}
		return cells[i][1] < cells[j][1]
	})
	return marshalString(cells)
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

	return summaryData{
		FinalTick:       m.tick,
		Robots:          len(m.robots),
		RobotsDestroyed: m.destroyed,
		Buildings:       len(m.buildings),
		BuildingsByType: byType,
		OreMined:        oreMined,
		OreStored:       oreStored,
		MetalMined:      metalMined,
		MetalStored:     metalStored,
		SpotsFound:      spots,
		DiscoveredCells: len(m.discovered),
		BaseLevel:       baseLevel,
	}
}

func statPair(stats map[string]any, key string) (mined, stored int) {
	res, _ := stats[key].(map[string]any)
	if res == nil {
		return 0, 0
	}
	return int(toInt(res["mined"])), int(toInt(res["stored"]))
}
