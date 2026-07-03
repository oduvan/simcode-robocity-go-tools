package engine

import "encoding/json"

// live-snapshot parse shapes (the public world-state doc the MCP get_world_state
// tool returns). Lossy by construction: fog-of-war, no hidden spot richness, no
// in-flight command internals — so a from-live run is an APPROXIMATE preview.

type liveSpot struct {
	Resource  string `json:"resource"`
	Remaining int    `json:"remaining"`
}
type liveInv struct {
	Ore      int `json:"ore"`
	Metal    int `json:"metal"`
	Capacity int `json:"capacity"`
}
type liveStorage struct {
	Ore      int `json:"ore"`
	Metal    int `json:"metal"`
	Capacity int `json:"capacity"`
}
type liveCons struct {
	Required  map[string]int `json:"required"`
	Delivered map[string]int `json:"delivered"`
	Progress  float64        `json:"progress"`
}
type liveTile struct {
	X    int       `json:"x"`
	Y    int       `json:"y"`
	Spot *liveSpot `json:"spot"`
}
type liveBuilding struct {
	ID           string       `json:"id"`
	Type         string       `json:"type"`
	Pos          [2]int       `json:"pos"`
	Status       string       `json:"status"`
	Storage      *liveStorage `json:"storage"`
	Construction *liveCons    `json:"construction"`
}
type liveRobot struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	Pos       [2]float64 `json:"pos"`
	Facing    string     `json:"facing"`
	Inventory *liveInv   `json:"inventory"`
	Energy    *float64   `json:"energy"`
}
type liveWorld struct {
	Seed int64 `json:"seed"`
}

// LiveSnapshot is the parsed public world-state document.
type LiveSnapshot struct {
	World      liveWorld      `json:"world"`
	Tiles      []liveTile     `json:"tiles"`
	Discovered [][2]int       `json:"discovered"`
	Buildings  []liveBuilding `json:"buildings"`
	Robots     []liveRobot    `json:"robots"`
}

// ParseLiveSnapshot decodes the world-state JSON returned by the MCP tool.
func ParseLiveSnapshot(raw []byte) (LiveSnapshot, error) {
	var s LiveSnapshot
	err := json.Unmarshal(raw, &s)
	return s, err
}

// SeedFromSnapshot overlays a fetched public snapshot onto a fresh canonical
// world (same seed) so hidden cells stay consistent, then overwrites discovered
// tiles, robots and buildings with the observed state. APPROXIMATE — see docstring.
func (m *Module) SeedFromSnapshot(snap LiveSnapshot) {
	wd := m.wd
	// Reset dynamic entities; keep the lazily-generated cell field.
	wd.robots = map[string]*robot{}
	wd.robotOrd = nil
	wd.buildings = map[string]*building{}
	wd.buildOrd = nil
	wd.pendingSpawn = nil

	for _, t := range snap.Tiles {
		cl := wd.cellAt(t.X, t.Y)
		if t.Spot != nil {
			cl.spot = &spot{resource: t.Spot.Resource, remaining: t.Spot.Remaining}
		} else {
			cl.spot = nil
		}
		wd.discovered[[2]int{t.X, t.Y}] = true
		wd.growBounds(t.X, t.Y)
	}
	for _, c := range snap.Discovered {
		wd.cellAt(c[0], c[1])
		wd.discovered[[2]int{c[0], c[1]}] = true
		wd.growBounds(c[0], c[1])
	}

	for _, b := range snap.Buildings {
		nb := &building{id: b.ID, typ: b.Type, pos: b.Pos, status: b.Status}
		if nb.status == "" {
			nb.status = StatusActive
		}
		if b.Storage != nil {
			nb.hasStorage = true
			nb.ore = b.Storage.Ore
			nb.metal = b.Storage.Metal
			nb.cap = b.Storage.Capacity
		}
		if nb.typ == BuildingMining {
			nb.spotCell = &[2]int{nb.pos[0], nb.pos[1]}
		}
		if b.Construction != nil && nb.status == StatusConstructing {
			nb.cons = &construction{
				targetType: nb.typ,
				reqOre:     b.Construction.Required["ore"],
				reqMetal:   b.Construction.Required["metal"],
				gotOre:     b.Construction.Delivered["ore"],
				gotMetal:   b.Construction.Delivered["metal"],
				progress:   b.Construction.Progress,
				buildTicks: 1,
			}
		}
		wd.addBuilding(nb)
	}

	for _, r := range snap.Robots {
		nr := &robot{
			id: r.ID, typ: r.Type, pos: r.Pos, face: r.Facing,
			cap: m.cfg.CarryCapacity, energy: m.cfg.EnergyCap, state: StateIdle,
		}
		if nr.typ == "" {
			nr.typ = "builder"
		}
		if nr.face == "" {
			nr.face = "S"
		}
		if r.Inventory != nil {
			nr.ore = r.Inventory.Ore
			nr.metal = r.Inventory.Metal
			if r.Inventory.Capacity > 0 {
				nr.cap = r.Inventory.Capacity
			}
		}
		if r.Energy != nil {
			nr.energy = *r.Energy
		}
		wd.addRobot(nr)
		wd.pendingSpawn = append(wd.pendingSpawn, nr.id)
	}
}
