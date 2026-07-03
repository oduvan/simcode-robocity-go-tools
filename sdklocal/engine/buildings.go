package engine

// advanceProduction runs the Base's robot factory.
func (m *Module) advanceProduction(tick int64) {
	wd := m.wd
	b := wd.base()
	if b == nil {
		return
	}
	if !b.prodActive && b.prodQueue > 0 {
		if b.ore >= m.cfg.RobotRecipe.Ore && b.metal >= m.cfg.RobotRecipe.Metal {
			b.ore -= m.cfg.RobotRecipe.Ore
			b.metal -= m.cfg.RobotRecipe.Metal
			b.prodActive = true
			b.prodProgress = 0
		}
	}
	if b.prodActive {
		b.prodProgress++
		if b.prodProgress >= m.cfg.RobotRecipe.BuildTicks {
			pos := wd.freeAdjacent(b.pos[0], b.pos[1])
			wd.nextRobot++
			id := "r" + itoa(wd.nextRobot)
			nr := &robot{
				id: id, typ: "builder", pos: [2]float64{float64(pos[0]), float64(pos[1])}, face: "S",
				cap: m.cfg.CarryCapacity, state: StateIdle, energy: m.cfg.EnergyCap,
				ore: m.cfg.ProducedOre, metal: m.cfg.ProducedMetal,
			}
			wd.addRobot(nr)
			wd.reveal(pos[0], pos[1], m.cfg.InitialReveal)
			b.prodActive = false
			b.prodProgress = 0
			if b.prodQueue > 0 {
				b.prodQueue--
			}
			m.emit(EventRobotProduced, id, tick, map[string]any{"robot_id": id})
			m.feedAdd(FeedEvent{Kind: EventRobotProduced, Robot: id})
			m.emit(EventSpawn, id, tick, nil)
		}
	}
}

// advanceMining runs autonomous extraction: every active Mining building drains
// its bound spot into its own local store at MiningSpeed/tick. Deterministic order.
func (m *Module) advanceMining(tick int64) {
	wd := m.wd
	for _, id := range wd.buildOrd {
		b := wd.buildings[id]
		if b == nil || b.typ != BuildingMining || b.status != StatusActive {
			continue
		}
		if b.spotCell == nil {
			continue
		}
		cl := wd.cellAt(b.spotCell[0], b.spotCell[1])
		if cl.spot == nil || cl.spot.remaining <= 0 {
			continue
		}
		room := b.cap - (b.ore + b.metal)
		if room <= 0 {
			if !b.fullEmitted {
				b.fullEmitted = true
				m.emit(EventStorageFull, "", tick, map[string]any{"building_id": b.id})
				m.feedAdd(FeedEvent{Kind: EventStorageFull})
			}
			continue
		}
		amount := minInt(m.cfg.MiningSpeed, minInt(cl.spot.remaining, room))
		cl.spot.remaining -= amount
		if cl.spot.resource == "ore" {
			b.ore += amount
			wd.oreMined += amount
		} else {
			b.metal += amount
			wd.metalMined += amount
		}
		if cl.spot.remaining <= 0 {
			cl.spot.depleted = true
			m.emit(EventSpotDepleted, "", tick, map[string]any{"building_id": b.id})
			m.feedAdd(FeedEvent{Kind: EventSpotDepleted})
		}
	}
}

// advanceConstructions progresses every fulfilled construction site.
func (m *Module) advanceConstructions(tick int64) {
	wd := m.wd
	ids := append([]string(nil), wd.buildOrd...)
	for _, id := range ids {
		b := wd.buildings[id]
		if b == nil || b.status != StatusConstructing || b.cons == nil {
			continue
		}
		if !b.cons.fulfilled() {
			continue
		}
		bt := b.cons.buildTicks
		if bt < 1 {
			bt = 1
		}
		b.cons.progress += 1.0 / float64(bt)
		if b.cons.progress >= 1.0 {
			m.completeConstruction(b, tick)
		}
	}
}

// completeConstruction flips a finished site to an active building under a NEW id.
func (m *Module) completeConstruction(plat *building, tick int64) {
	wd := m.wd
	typ := plat.cons.targetType
	pos := plat.pos

	wd.nextBuild++
	newID := typ + "-" + itoa(wd.nextBuild)
	nb := &building{id: newID, typ: typ, pos: pos, status: StatusActive}
	switch typ {
	case BuildingMining:
		nb.hasStorage = true
		nb.cap = m.cfg.MiningStorageCap
		nb.spotCell = &[2]int{pos[0], pos[1]}
	case BuildingStorage:
		nb.hasStorage = true
		nb.cap = m.cfg.StorageCap
	case BuildingFlyingStation:
		// no storage — robots land here and charge via the `charge` command.
	}

	wd.removeBuilding(plat.id)
	wd.addBuilding(nb)

	m.emit(EventConstructionComplete, "", tick, map[string]any{"building_id": newID, "type": typ})
	m.feedAdd(FeedEvent{Kind: EventConstructionComplete})
}
