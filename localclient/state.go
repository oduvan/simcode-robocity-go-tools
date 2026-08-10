// The live read model, backed by Redis city.<id>.state.*. This is the Go port
// of clients/python/simcode/_state.py. Each top-level read (City.Robot / Buildings /
// World / Base) takes a fresh one-shot read of the state store and decodes the
// same JSON the Python reader decodes (and that GAME writes, per
// game/core/contract/schema.go).
//
// State store layout (each key is a plain JSON string, not a hash):
//
//	city.<id>.state.meta       {"tick","seq","city"}
//	city.<id>.state.world      {"size":[w,h],"seed"}
//	city.<id>.state.robots     [{"id","type","pos":[x,y],"facing","inventory",
//	                            "state","command"}]
//	city.<id>.state.buildings  [{"id","type","pos","status","storage",
//	                            "spot"|"production"|"construction"}]
//	city.<id>.state.spots      [[x,y,resource,remaining], ...]  (sparse: only cells
//	                           that carry a deposit; remaining 0 = depleted)
//	city.<id>.state.discovered [[y,x0,x1], ...]  per-row INCLUSIVE runs of revealed
//	                           cells — a map is overwhelmingly contiguous, so this
//	                           is a fraction of the size of one record per cell
package simcode

import (
	"encoding/json"
	"errors"
)

// ----------------------------------------------------------------------------
// value views (mirror _state.py Store / Spot)
// ----------------------------------------------------------------------------

// Store is a multi-item resource bag with a shared capacity — used for both a
// robot's carried inventory and a building's storage. It decodes the wire shape
// {"items":{item:qty,...},"capacity":N}.
type Store struct {
	Items    map[string]int `json:"items"`
	Capacity int            `json:"capacity"`
}

// Total is the sum of all stored item quantities.
func (s Store) Total() int {
	t := 0
	for _, v := range s.Items {
		t += v
	}
	return t
}

// Free is the remaining capacity (never negative).
func (s Store) Free() int {
	f := s.Capacity - s.Total()
	if f < 0 {
		return 0
	}
	return f
}

// Get returns the stored quantity of item (0 if absent).
func (s Store) Get(item string) int { return s.Items[item] }

// Has reports whether the store holds a positive quantity of item.
func (s Store) Has(item string) bool { return s.Items[item] > 0 }

// IsFull reports whether the store can hold no more.
func (s Store) IsFull() bool { return s.Free() <= 0 }

// storeOrEmpty dereferences a decoded *Store, guaranteeing a non-nil Items map
// so callers can index Get/Has safely even when the wire field was absent.
func storeOrEmpty(s *Store) Store {
	if s == nil {
		return Store{Items: map[string]int{}}
	}
	if s.Items == nil {
		return Store{Items: map[string]int{}, Capacity: s.Capacity}
	}
	return *s
}

// storePtr normalizes an optional *Store: nil stays nil (the wire field was
// absent — e.g. a non-processor has no input/output pool), but a present store
// is guaranteed a non-nil Items map so Get/Has are safe to index.
func storePtr(s *Store) *Store {
	if s == nil {
		return nil
	}
	if s.Items == nil {
		return &Store{Items: map[string]int{}, Capacity: s.Capacity}
	}
	return s
}

// Spot is a finite resource deposit on a tile / under a Mining building.
type Spot struct {
	Resource  string `json:"resource"`
	Remaining int    `json:"remaining"`
}

// recipeView is the wire decode of a processor building's fixed conversion
// (schema.go RecipeView). Surfaced to user code through Building.Recipe.
type recipeView struct {
	Inputs    map[string]int `json:"inputs"`
	Output    string         `json:"output"`
	OutAmount int            `json:"out_amount"`
	Ticks     int            `json:"ticks"`
}

// ----------------------------------------------------------------------------
// raw decoded state (mirror of the JSON GAME writes)
// ----------------------------------------------------------------------------

type robotState struct {
	ID        string      `json:"id"`
	Type      string      `json:"type"`
	Pos       *[2]float64 `json:"pos"`
	Facing    string      `json:"facing"`
	Inventory *Store      `json:"inventory"`
	Energy    *float64    `json:"energy"`
	State     string      `json:"state"`
	Command   string      `json:"command"`
	// Living economy (#42): remaining/max cumulative flight distance (lifespan).
	// When LifeRemaining reaches 0 the robot expires (robot_expired).
	LifeRemaining *float64 `json:"life_remaining"`
	LifeMax       *float64 `json:"life_max"`
}

type buildingState struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Pos          *[2]int        `json:"pos"`
	W            int            `json:"w"`
	H            int            `json:"h"`
	Status       string         `json:"status"`
	Progress     *float64       `json:"progress"`
	Storage      *Store         `json:"storage"`
	Spot         *Spot          `json:"spot"`
	Production   map[string]any `json:"production"`
	Construction map[string]any `json:"construction"`
	Level        int            `json:"level"` // Base only: the objective level
	Quest        map[string]any `json:"quest"` // Base only: {required,progress} raw bag
	// Supply-chain (#5): processor input/output pools, its fixed recipe, and the
	// recoverable materials store while decommissioning. All nil on non-processors.
	Input       *Store      `json:"input"`
	Output      *Store      `json:"output"`
	Recipe      *recipeView `json:"recipe"`
	Recoverable *Store      `json:"recoverable"`
	// Living economy (#42): Condition (0-100) on a WEARING T2/T3 processor — nil on
	// buildings that never wear. Unlocks (Base only) is the set of building + robot
	// types buildable at the Base's current level.
	Condition *int     `json:"condition"`
	Unlocks   []string `json:"unlocks"`
}

// spotState is one entry of state.spots, on the wire as [x, y, resource, remaining].
type spotState struct {
	X, Y      int
	Resource  string
	Remaining int
}

func (s *spotState) UnmarshalJSON(b []byte) error {
	var a []any
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	if len(a) != 4 {
		return errors.New("spot entry: want [x,y,resource,remaining]")
	}
	x, _ := a[0].(float64)
	y, _ := a[1].(float64)
	res, _ := a[2].(string)
	rem, _ := a[3].(float64)
	s.X, s.Y, s.Resource, s.Remaining = int(x), int(y), res, int(rem)
	return nil
}

// runState is one entry of state.discovered: [y, x0, x1] inclusive.
type runState struct{ Y, X0, X1 int }

func (r *runState) UnmarshalJSON(b []byte) error {
	var a [3]int
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	r.Y, r.X0, r.X1 = a[0], a[1], a[2]
	return nil
}

// tileState is the SYNTHESIZED per-cell view the client API still exposes (r.here,
// world.Spots). It is no longer a wire type: terrain is a module constant and the
// spot is looked up from the sparse list.
type tileState struct {
	X       int
	Y       int
	Terrain string
	Spot    *Spot
}

type metaState struct {
	Tick int64  `json:"tick"`
	Seq  int64  `json:"seq"`
	City string `json:"city"`
}

type worldState struct {
	Size    *[2]int `json:"size"`   // discovered bounding-box extent (endless world)
	Origin  *[2]int `json:"origin"` // min (x,y) of the discovered region
	Seed    int64   `json:"seed"`
	Endless bool    `json:"endless"`
}

// snapshot is a one-shot parse of state.* for a single read, indexed by id and
// "x,y" — the Go equivalent of _state.StateReader.
type snapshot struct {
	meta       metaState
	world      worldState
	robots     map[string]robotState
	buildings  map[string]buildingState
	spots      map[string]spotState
	discovered map[string]bool // expanded from runs; "x,y"
	discRaw    string          // the raw runs doc, exposed by World.Discovered()
}

// decodeSnapshot builds a snapshot from the raw values MGET'd in stateKeys
// order (meta, world, robots, buildings, tiles, discovered). Missing keys parse
// to zero values, matching the Python reader's defaults.
func decodeSnapshot(vals []string) snapshot {
	get := func(i int) string {
		if i < len(vals) {
			return vals[i]
		}
		return ""
	}
	s := snapshot{
		robots:     map[string]robotState{},
		buildings:  map[string]buildingState{},
		spots:      map[string]spotState{},
		discovered: map[string]bool{},
	}
	if v := get(0); v != "" {
		_ = json.Unmarshal([]byte(v), &s.meta)
	}
	if v := get(1); v != "" {
		_ = json.Unmarshal([]byte(v), &s.world)
	}
	if v := get(2); v != "" {
		var rs []robotState
		if json.Unmarshal([]byte(v), &rs) == nil {
			for _, r := range rs {
				if r.ID != "" {
					s.robots[r.ID] = r
				}
			}
		}
	}
	if v := get(3); v != "" {
		var bs []buildingState
		if json.Unmarshal([]byte(v), &bs) == nil {
			for _, b := range bs {
				if b.ID != "" {
					s.buildings[b.ID] = b
				}
			}
		}
	}
	if v := get(4); v != "" {
		var sp []spotState
		if json.Unmarshal([]byte(v), &sp) == nil {
			for _, t := range sp {
				s.spots[tileKey(t.X, t.Y)] = t
			}
		}
	}
	s.discRaw = get(5)
	if s.discRaw != "" {
		var runs []runState
		if json.Unmarshal([]byte(s.discRaw), &runs) == nil {
			for _, r := range runs {
				for x := r.X0; x <= r.X1; x++ {
					s.discovered[tileKey(x, r.Y)] = true
				}
			}
		}
	}
	return s
}

func tileKey(x, y int) string {
	return itoa(x) + "," + itoa(y)
}

func itoa(n int) string {
	// small, allocation-light int->string (n is a grid coord)
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// tileAt synthesizes the per-cell view from the runs + sparse spots. A cell exists
// iff it has been revealed; its terrain is the module constant.
func (s snapshot) tileAt(x, y int) (tileState, bool) {
	k := tileKey(x, y)
	if !s.discovered[k] {
		return tileState{}, false
	}
	t := tileState{X: x, Y: y, Terrain: TerrainGround}
	if sp, ok := s.spots[k]; ok {
		t.Spot = &Spot{Resource: sp.Resource, Remaining: sp.Remaining}
	}
	return t, true
}

func (s snapshot) buildingAt(x, y int) *buildingState {
	for id, b := range s.buildings {
		if b.Pos == nil {
			continue
		}
		// Pos is the min corner; the building covers its whole w×h footprint, so
		// (x,y) hits it if it lies anywhere inside that box.
		w, h := b.W, b.H
		if w < 1 {
			w = 1
		}
		if h < 1 {
			h = 1
		}
		if b.Pos[0] <= x && x < b.Pos[0]+w && b.Pos[1] <= y && y < b.Pos[1]+h {
			bb := s.buildings[id]
			return &bb
		}
	}
	return nil
}
