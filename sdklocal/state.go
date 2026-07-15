// The live read model. Copied verbatim from the published SDK
// (github.com/lyabah/simcode-sdk-go, state.go). Each top-level read (City.Robot /
// Buildings / World / Base) decodes the same JSON the server writes to state.*
// (game/core/contract/schema.go) — here produced in-process by the engine — so a
// handle reflects the current tick when the handler runs.
package simcode

import "encoding/json"

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

type robotState struct {
	ID        string      `json:"id"`
	Type      string      `json:"type"`
	Pos       *[2]float64 `json:"pos"`
	Facing    string      `json:"facing"`
	Inventory *Store      `json:"inventory"`
	Energy    *float64    `json:"energy"`
	State     string      `json:"state"`
	Command   string      `json:"command"`
}

type buildingState struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Pos          *[2]int        `json:"pos"`
	Status       string         `json:"status"`
	Progress     *float64       `json:"progress"`
	Storage      *Store         `json:"storage"`
	Spot         *Spot          `json:"spot"`
	Production   map[string]any `json:"production"`
	Construction map[string]any `json:"construction"`
	Level        int            `json:"level"`          // Base only: the objective level (1+)
	Quest        map[string]any `json:"quest"`          // Base only: {required, progress}
	// Supply-chain (#5): processor input/output pools, its fixed recipe, and the
	// recoverable materials store while decommissioning. All nil on non-processors.
	Input       *Store      `json:"input"`
	Output      *Store      `json:"output"`
	Recipe      *recipeView `json:"recipe"`
	Recoverable *Store      `json:"recoverable"`
}

type tileState struct {
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Terrain string `json:"terrain"`
	Spot    *Spot  `json:"spot"`
}

type metaState struct {
	Tick int64  `json:"tick"`
	Seq  int64  `json:"seq"`
	City string `json:"city"`
}

type worldState struct {
	Size    *[2]int `json:"size"`
	Origin  *[2]int `json:"origin"`
	Seed    int64   `json:"seed"`
	Endless bool    `json:"endless"`
}

// snapshot is a one-shot parse of state.* for a single read, indexed by id and "x,y".
type snapshot struct {
	meta       metaState
	world      worldState
	robots     map[string]robotState
	buildings  map[string]buildingState
	tiles      map[string]tileState
	discovered string
}

// decodeSnapshot builds a snapshot from the raw values in stateKeys order (meta,
// world, robots, buildings, tiles, discovered). Missing keys parse to zero values.
func decodeSnapshot(vals []string) snapshot {
	get := func(i int) string {
		if i < len(vals) {
			return vals[i]
		}
		return ""
	}
	s := snapshot{
		robots:    map[string]robotState{},
		buildings: map[string]buildingState{},
		tiles:     map[string]tileState{},
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
		var ts []tileState
		if json.Unmarshal([]byte(v), &ts) == nil {
			for _, t := range ts {
				s.tiles[tileKey(t.X, t.Y)] = t
			}
		}
	}
	s.discovered = get(5)
	return s
}

func tileKey(x, y int) string {
	return itoa(x) + "," + itoa(y)
}

func itoa(n int) string {
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

func (s snapshot) tileAt(x, y int) (tileState, bool) {
	t, ok := s.tiles[tileKey(x, y)]
	return t, ok
}

func (s snapshot) buildingAt(x, y int) *buildingState {
	for id, b := range s.buildings {
		if b.Pos != nil && b.Pos[0] == x && b.Pos[1] == y {
			bb := s.buildings[id]
			return &bb
		}
	}
	return nil
}
