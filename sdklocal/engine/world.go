package engine

import "math"

// cell is one grid tile. terrain is uniform "ground" in v1; spot is the optional
// finite deposit; building is the optional structure id occupying it. The world
// is endless: cells are materialized on demand by cellAt as they are revealed.
type cell struct {
	terrain  string
	spot     *spot
	building string
}

// spot is a finite resource deposit on a cell. A Mining building drains it.
type spot struct {
	resource  string // "ore" | "metal"
	remaining int
	depleted  bool
}

// activeCmd is a robot's in-flight command (one at a time; a queue holds the rest).
// args are already-parsed native values (float64/int/string), matching the SDK
// call, mirroring the Python port (which also receives decoded values).
type activeCmd struct {
	cmd    string
	args   []any
	target [2]float64
}

// robot is a flying unit over continuous coordinates.
type robot struct {
	id     string
	typ    string
	pos    [2]float64
	face   string
	ore    int
	metal  int
	cap    int
	energy float64
	state  string
	cmd    *activeCmd
	queue  []*activeCmd

	idleEmittedTick int64
}

func (r *robot) carried() int { return r.ore + r.metal }
func (r *robot) free() int    { return r.cap - r.carried() }
func (r *robot) command() string {
	if r.cmd == nil {
		return ""
	}
	return r.cmd.cmd
}

// cellF returns the robot's rounded integer cell.
func (r *robot) cellF() [2]int {
	return [2]int{int(math.Round(r.pos[0])), int(math.Round(r.pos[1]))}
}

// construction is the in-progress block on a building with status "constructing".
type construction struct {
	targetType string
	reqOre     int
	reqMetal   int
	gotOre     int
	gotMetal   int
	progress   float64
	buildTicks int
}

func (c *construction) fulfilled() bool {
	return c.gotOre >= c.reqOre && c.gotMetal >= c.reqMetal
}

// building is a structure (Base, Mining, Storage, Flying Station, or a
// constructing site).
type building struct {
	id     string
	typ    string
	pos    [2]int // anchor = MIN corner of the footprint
	w, h   int    // footprint size in cells (>=1); occupies [x,x+w)×[y,y+h)
	status string

	hasStorage  bool
	ore         int
	metal       int
	cap         int
	fullEmitted bool

	spotCell *[2]int

	prodQueue    int
	prodActive   bool
	prodProgress int

	cons *construction
}

// world is the full simulation state for one city. The grid is endless.
type world struct {
	cfg  Config
	city string
	seed int64

	cells map[[2]int]*cell

	robots    map[string]*robot
	robotOrd  []string
	buildings map[string]*building
	buildOrd  []string

	discovered map[[2]int]bool

	minX, minY, maxX, maxY int
	haveBounds             bool

	nextRobot int
	nextBuild int

	oreMined   int
	metalMined int

	pendingSpawn []string
}

func newWorld(cfg Config) *world {
	return &world{
		cfg:        cfg,
		cells:      map[[2]int]*cell{},
		robots:     map[string]*robot{},
		buildings:  map[string]*building{},
		discovered: map[[2]int]bool{},
	}
}

// generate builds the deterministic starting world.
func (wd *world) generate(city string, seed int64) {
	wd.city = city
	wd.seed = seed

	base := &building{
		id: "base-1", typ: BuildingBase, pos: [2]int{0, 0},
		status: StatusActive, hasStorage: true, cap: wd.cfg.BaseStorageCap,
	}
	wd.cellAt(0, 0).spot = nil
	wd.addBuilding(base)

	num := wd.cfg.NumStartRobots
	if num < 1 {
		num = 1
	}
	for i := 0; i < num; i++ {
		off := ringOffset(i)
		pos := [2]float64{float64(off[0]), float64(off[1])}
		wd.nextRobot++
		r := &robot{
			id: "r" + itoa(wd.nextRobot), typ: "builder", pos: pos, face: "S",
			cap: wd.cfg.CarryCapacity, state: StateIdle, energy: wd.cfg.EnergyCap,
			ore: wd.cfg.StartOre, metal: wd.cfg.StartMetal,
		}
		wd.addRobot(r)
		wd.reveal(off[0], off[1], wd.cfg.InitialReveal)
		wd.pendingSpawn = append(wd.pendingSpawn, r.id)
	}

	wd.reveal(0, 0, wd.cfg.InitialReveal)
}

// ringOffset returns a deterministic integer offset for the i-th starting robot.
func ringOffset(i int) [2]int {
	offs := [][2]int{
		{1, 0}, {-1, 0}, {0, 1}, {0, -1},
		{1, 1}, {-1, -1}, {1, -1}, {-1, 1},
		{2, 0}, {-2, 0}, {0, 2}, {0, -2},
	}
	if i < len(offs) {
		return offs[i]
	}
	return [2]int{i - len(offs) + 3, 0}
}

// cellAt returns the (materialized) cell at (x,y), generating it deterministically
// on first access. Generation depends only on (seed,x,y).
func (wd *world) cellAt(x, y int) *cell {
	key := [2]int{x, y}
	if c := wd.cells[key]; c != nil {
		return c
	}
	c := &cell{terrain: "ground"}
	h := hashCell(wd.seed, x, y)
	if float64(h%1_000_000)/1_000_000.0 < wd.cfg.SpotDensity {
		res := "ore"
		if (h>>21)&1 == 1 {
			res = "metal"
		}
		rich := wd.cfg.SpotRichMin
		if span := wd.cfg.SpotRichMax - wd.cfg.SpotRichMin; span > 0 {
			rich += int((h >> 24) % uint64(span+1))
		}
		c.spot = &spot{resource: res, remaining: rich}
	}
	wd.cells[key] = c
	return c
}

// hashCell is a deterministic 64-bit mix of (seed,x,y) — a SplitMix64-style
// finalizer over the folded coordinates. Must stay byte-identical to the Go source.
func hashCell(seed int64, x, y int) uint64 {
	z := uint64(seed)*0x9E3779B97F4A7C15 ^ uint64(int64(x))*0xD1B54A32D192ED03 ^ uint64(int64(y))*0xF58CCF12EAF4B57B
	z += 0x9E3779B97F4A7C15
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	z = z ^ (z >> 31)
	return z
}

func (wd *world) addRobot(r *robot) {
	wd.robots[r.id] = r
	wd.robotOrd = append(wd.robotOrd, r.id)
	if n := robotNum(r.id); n > wd.nextRobot {
		wd.nextRobot = n
	}
}

func (wd *world) removeRobot(id string) {
	if _, ok := wd.robots[id]; !ok {
		return
	}
	delete(wd.robots, id)
	for i, v := range wd.robotOrd {
		if v == id {
			wd.robotOrd = append(wd.robotOrd[:i], wd.robotOrd[i+1:]...)
			break
		}
	}
}

func (wd *world) addBuilding(b *building) {
	// Derive the footprint from config when the caller left it unset; every
	// building covers at least its anchor cell. Occupy every covered cell.
	if b.w < 1 || b.h < 1 {
		b.w, b.h = wd.cfg.footprint(b.typ)
	}
	wd.buildings[b.id] = b
	wd.buildOrd = append(wd.buildOrd, b.id)
	for y := b.pos[1]; y < b.pos[1]+b.h; y++ {
		for x := b.pos[0]; x < b.pos[0]+b.w; x++ {
			wd.cellAt(x, y).building = b.id
		}
	}
}

func (wd *world) removeBuilding(id string) {
	b := wd.buildings[id]
	if b == nil {
		return
	}
	w, h := b.w, b.h
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	for y := b.pos[1]; y < b.pos[1]+h; y++ {
		for x := b.pos[0]; x < b.pos[0]+w; x++ {
			if cl := wd.cellAt(x, y); cl.building == id {
				cl.building = ""
			}
		}
	}
	delete(wd.buildings, id)
	for i, v := range wd.buildOrd {
		if v == id {
			wd.buildOrd = append(wd.buildOrd[:i], wd.buildOrd[i+1:]...)
			break
		}
	}
}

func robotNum(id string) int {
	n := 0
	for _, c := range id {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

func (wd *world) base() *building {
	for _, id := range wd.buildOrd {
		if b := wd.buildings[id]; b.typ == BuildingBase {
			return b
		}
	}
	return nil
}

func (wd *world) buildingAt(x, y int) *building {
	if c := wd.cells[[2]int{x, y}]; c != nil && c.building != "" {
		return wd.buildings[c.building]
	}
	return nil
}

// footprintFree reports whether every cell of the w×h rectangle anchored at
// (x,y) is currently free of any building (used to validate placement).
func (wd *world) footprintFree(x, y, w, h int) bool {
	for cy := y; cy < y+h; cy++ {
		for cx := x; cx < x+w; cx++ {
			if wd.buildingAt(cx, cy) != nil {
				return false
			}
		}
	}
	return true
}

// reveal marks all cells within Chebyshev radius r of (cx,cy) as discovered.
func (wd *world) reveal(cx, cy, r int) {
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			wd.cellAt(x, y)
			wd.discovered[[2]int{x, y}] = true
			wd.growBounds(x, y)
		}
	}
}

func (wd *world) growBounds(x, y int) {
	if !wd.haveBounds {
		wd.minX, wd.maxX, wd.minY, wd.maxY = x, x, y, y
		wd.haveBounds = true
		return
	}
	if x < wd.minX {
		wd.minX = x
	}
	if x > wd.maxX {
		wd.maxX = x
	}
	if y < wd.minY {
		wd.minY = y
	}
	if y > wd.maxY {
		wd.maxY = y
	}
}

// freeAdjacent returns an integer cell next to (x,y) with no building on it, or
// (x,y) itself — deterministic order N,E,S,W.
func (wd *world) freeAdjacent(x, y int) [2]int {
	for _, d := range [][2]int{{0, -1}, {1, 0}, {0, 1}, {-1, 0}} {
		nx, ny := x+d[0], y+d[1]
		if wd.buildingAt(nx, ny) == nil {
			return [2]int{nx, ny}
		}
	}
	return [2]int{x, y}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
