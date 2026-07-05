package engine

import "math"

// beginCmd starts the robot's active command. Returns true when it resolved
// immediately, false when a timed command (move_to flight, or charge) is now in
// progress.
func (m *Module) beginCmd(r *robot, tick int64) bool {
	switch r.cmd.cmd {

	case CmdMoveTo:
		x := argFloat(r.cmd.args, 0, r.pos[0])
		y := argFloat(r.cmd.args, 1, r.pos[1])
		r.cmd.target = [2]float64{x, y}
		r.state = StateMoving
		return false

	case CmdCharge:
		if m.stationAt(r.cellF()) == nil {
			m.blocked(r, tick, "no_station")
			return true
		}
		if r.energy >= m.cfg.EnergyCap {
			m.emit(EventChargeComplete, r.id, tick, map[string]any{"energy": r.energy})
			m.feedAdd(FeedEvent{Kind: EventChargeComplete, Robot: r.id})
			r.state = StateIdle
			return true
		}
		r.state = StateCharging
		return false

	case CmdDrop:
		m.doDrop(r, tick)
		return true

	case CmdPickUp:
		m.doPickUp(r, tick)
		return true

	case CmdSend:
		m.doSend(r, tick)
		return true

	case CmdCancel:
		return true
	}
	return true
}

func (m *Module) blocked(r *robot, tick int64, reason string) {
	m.emit(EventBlocked, r.id, tick, map[string]any{"reason": reason})
	m.feedAdd(FeedEvent{Kind: EventBlocked, Robot: r.id})
	r.state = StateIdle
}

// stationAt returns an active building on cell c that recharges robots: a Flying
// Station OR the Base itself (the Base doubles as a charging pad).
func (m *Module) stationAt(c [2]int) *building {
	b := m.wd.buildingAt(c[0], c[1])
	if b != nil && b.status == StatusActive &&
		(b.typ == BuildingFlyingStation || b.typ == BuildingBase) {
		return b
	}
	return nil
}

// advanceMove flies the robot toward its target, spending energy proportional to
// distance flown. Running dry mid-flight destroys it (cargo lost).
func (m *Module) advanceMove(r *robot, tick int64) {
	wd := m.wd
	dx := r.cmd.target[0] - r.pos[0]
	dy := r.cmd.target[1] - r.pos[1]
	dist := math.Hypot(dx, dy)
	if dist < 1e-9 {
		m.arriveMove(r, tick)
		return
	}
	r.face = faceOfF(dx, dy)

	move := m.cfg.FlySpeed
	if move > dist {
		move = dist
	}
	cost := move * m.cfg.EnergyPerDistance

	if cost > r.energy {
		reach := 0.0
		if m.cfg.EnergyPerDistance > 0 {
			reach = r.energy / m.cfg.EnergyPerDistance
		}
		frac := reach / dist
		r.pos[0] += dx * frac
		r.pos[1] += dy * frac
		r.energy = 0
		wd.reveal(r.cellF()[0], r.cellF()[1], m.cfg.MoveReveal)
		m.destroyRobot(r, tick, "out_of_energy")
		return
	}

	r.energy -= cost
	frac := move / dist
	r.pos[0] += dx * frac
	r.pos[1] += dy * frac
	wd.reveal(r.cellF()[0], r.cellF()[1], m.cfg.MoveReveal)

	if move >= dist-1e-9 {
		r.pos = r.cmd.target
		m.arriveMove(r, tick)
	}
}

func (m *Module) arriveMove(r *robot, tick int64) {
	m.emit(EventArrived, r.id, tick, map[string]any{"position": [2]float64{r.pos[0], r.pos[1]}})
	m.finishCmd(r, tick)
}

func (m *Module) destroyRobot(r *robot, tick int64, reason string) {
	m.emit(EventRobotDestroyed, r.id, tick, map[string]any{"position": [2]float64{r.pos[0], r.pos[1]}, "reason": reason})
	m.feedAdd(FeedEvent{Kind: EventRobotDestroyed, Robot: r.id})
	m.wd.removeRobot(r.id)
}

// advanceCharge recharges a robot parked on a Flying Station. Completes at full.
func (m *Module) advanceCharge(r *robot, tick int64) {
	if m.stationAt(r.cellF()) == nil {
		m.blocked(r, tick, "left_station")
		m.finishCmd(r, tick)
		return
	}
	r.energy += m.cfg.ChargeRate
	if r.energy >= m.cfg.EnergyCap {
		r.energy = m.cfg.EnergyCap
		m.emit(EventChargeComplete, r.id, tick, map[string]any{"energy": r.energy})
		m.feedAdd(FeedEvent{Kind: EventChargeComplete, Robot: r.id})
		m.finishCmd(r, tick)
	}
}

// doBuild places a construction site at (x,y) (world-scoped: world.build).
func (m *Module) doBuild(args []any, tick int64) {
	wd := m.wd
	typ := argStr(args, 0, "")
	x := argInt(args, 1, 0)
	y := argInt(args, 2, 0)

	// (x,y) is the anchor = min corner; the building occupies the whole w×h box.
	w, h := m.cfg.footprint(typ)
	reason := ""
	switch {
	case typ == BuildingBase:
		reason = "base_not_buildable"
	case !hasRecipe(m.cfg.Recipes, typ):
		reason = "unknown_type"
	case !wd.footprintFree(x, y, w, h):
		reason = "cell_occupied"
	}
	if reason == "" && typ == BuildingMining {
		if cl := wd.cellAt(x, y); cl.spot == nil || cl.spot.remaining <= 0 {
			reason = "no_spot"
		}
	}
	if reason != "" {
		m.emit(EventBlocked, "", tick, map[string]any{"reason": reason})
		m.feedAdd(FeedEvent{Kind: EventBlocked})
		return
	}

	recipe := m.cfg.Recipes[typ]
	wd.nextBuild++
	id := platID(wd.nextBuild)
	b := &building{
		id: id, typ: typ, pos: [2]int{x, y}, w: w, h: h, status: StatusConstructing,
		cons: &construction{
			targetType: typ,
			reqOre:     recipe.Ore,
			reqMetal:   recipe.Metal,
			buildTicks: recipe.BuildTicks,
		},
	}
	wd.addBuilding(b)
	wd.reveal(x, y, m.cfg.MoveReveal)
	m.emit(EventConstructionStarted, "", tick, map[string]any{"building_id": id, "type": typ})
	m.feedAdd(FeedEvent{Kind: EventConstructionStarted})
}

func hasRecipe(rs map[string]Recipe, typ string) bool {
	_, ok := rs[typ]
	return ok
}

// depositTarget returns the building a drop should go into.
func (m *Module) depositTarget(r *robot) *building {
	wd := m.wd
	c := r.cellF()
	if b := wd.buildingAt(c[0], c[1]); b != nil {
		return b
	}
	for _, d := range [4][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} {
		if b := wd.buildingAt(c[0]+d[0], c[1]+d[1]); b != nil && b.hasStorage {
			return b
		}
	}
	return nil
}

func (m *Module) doDrop(r *robot, tick int64) {
	b := m.depositTarget(r)
	if b == nil {
		m.blocked(r, tick, "nothing_here")
		return
	}
	ore, okO := optInt(r.cmd.args, 0)
	metal, okM := optInt(r.cmd.args, 1)
	if !okO {
		ore = r.ore
	}
	if !okM {
		metal = r.metal
	}
	ore = minInt(max0(ore), r.ore)
	metal = minInt(max0(metal), r.metal)

	if b.status == StatusConstructing && b.cons != nil {
		takeOre := max0(minInt(ore, b.cons.reqOre-b.cons.gotOre))
		takeMetal := max0(minInt(metal, b.cons.reqMetal-b.cons.gotMetal))
		b.cons.gotOre += takeOre
		b.cons.gotMetal += takeMetal
		r.ore -= takeOre
		r.metal -= takeMetal
		m.emit(EventResourceDelivered, r.id, tick, map[string]any{"building_id": b.id, "ore": takeOre, "metal": takeMetal})
		m.feedAdd(FeedEvent{Kind: EventResourceDelivered, Robot: r.id})
		r.state = StateIdle
		return
	}

	if !b.hasStorage {
		m.blocked(r, tick, "no_storage")
		return
	}
	room := b.cap - (b.ore + b.metal)
	takeOre := minInt(ore, room)
	room -= takeOre
	takeMetal := minInt(metal, room)
	b.ore += takeOre
	b.metal += takeMetal
	r.ore -= takeOre
	r.metal -= takeMetal
	m.emit(EventResourceDelivered, r.id, tick, map[string]any{"building_id": b.id, "ore": takeOre, "metal": takeMetal})
	m.feedAdd(FeedEvent{Kind: EventResourceDelivered, Robot: r.id})
	if b.ore+b.metal >= b.cap {
		m.emit(EventStorageFull, r.id, tick, map[string]any{"building_id": b.id})
		m.feedAdd(FeedEvent{Kind: EventStorageFull, Robot: r.id})
	}
	r.state = StateIdle
}

// doPickUp grabs resources from the building on the robot's cell (not the Base).
func (m *Module) doPickUp(r *robot, tick int64) {
	c := r.cellF()
	b := m.wd.buildingAt(c[0], c[1])
	if b == nil {
		m.blocked(r, tick, "nothing_here")
		return
	}
	if b.typ == BuildingBase {
		m.blocked(r, tick, "base_reserved")
		return
	}
	if !b.hasStorage {
		m.blocked(r, tick, "no_storage")
		return
	}
	ore, okO := optInt(r.cmd.args, 0)
	metal, okM := optInt(r.cmd.args, 1)
	if !okO {
		ore = b.ore
	}
	if !okM {
		metal = b.metal
	}
	ore = minInt(max0(ore), b.ore)
	metal = minInt(max0(metal), b.metal)
	takeOre := minInt(ore, r.free())
	takeMetal := minInt(metal, r.free()-takeOre)
	b.ore -= takeOre
	b.metal -= takeMetal
	r.ore += takeOre
	r.metal += takeMetal
	if b.fullEmitted && b.ore+b.metal < b.cap {
		b.fullEmitted = false
	}
	if r.free() == 0 {
		m.emit(EventInventoryFull, r.id, tick, nil)
		m.feedAdd(FeedEvent{Kind: EventInventoryFull, Robot: r.id})
	}
	r.state = StateIdle
}

func (m *Module) doSend(r *robot, tick int64) {
	target := argStr(r.cmd.args, 0, "")
	if _, ok := m.wd.robots[target]; !ok {
		m.blocked(r, tick, "no_target")
		return
	}
	var payload any
	if len(r.cmd.args) > 1 {
		payload = r.cmd.args[1]
	}
	m.emit(EventMessage, target, tick, map[string]any{"from": r.id, "payload": payload})
	m.feedAdd(FeedEvent{Kind: EventMessage, Robot: r.id})
	r.state = StateIdle
}

func max0(a int) int {
	if a < 0 {
		return 0
	}
	return a
}

func platID(n int) string { return "plat-" + itoa(n) }

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
