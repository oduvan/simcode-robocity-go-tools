package engine

// advanceStationProduction runs every Flying Station's robot factory. Each active
// station with a queue starts a unit when its OWN production store can pay
// RobotRecipe, accumulates build time, and on finish spawns a robot AT the station
// (empty inventory, full energy). Deterministic buildOrd order. (Mirror of
// buildings.go advanceStationProduction.)
func (m *Module) advanceStationProduction(tick int64) {
	wd := m.wd
	for _, id := range wd.buildOrd {
		b := wd.buildings[id]
		if b == nil || b.typ != BuildingFlyingStation || b.status != StatusActive {
			continue
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
				wd.nextRobot++
				rid := "r" + itoa(wd.nextRobot)
				// Spawns AT the station, empty (it already paid to build it).
				nr := &robot{
					id: rid, typ: "builder", pos: [2]float64{float64(b.pos[0]), float64(b.pos[1])}, face: "S",
					cap: m.cfg.CarryCapacity, state: StateIdle, energy: m.cfg.EnergyCap,
				}
				wd.addRobot(nr)
				wd.reveal(b.pos[0], b.pos[1], m.cfg.InitialReveal)
				b.prodActive = false
				b.prodProgress = 0
				if b.prodQueue > 0 {
					b.prodQueue--
				}
				m.emit(EventRobotProduced, rid, tick, map[string]any{"robot_id": rid})
				m.feedAdd(FeedEvent{Kind: EventRobotProduced, Robot: rid})
				m.emit(EventSpawn, rid, tick, nil)
			}
		}
	}
}

// advanceBaseQuest runs the Base's leveling (the game objective). It announces
// the current quest once, then — while the Base quest store satisfies the current
// quest — RESETS the store to 0 and levels the Base up, emitting base_level_up +
// quest_updated. Drops into the Base are capped per-resource at the requirement,
// so a met quest holds exactly the requirement and the reset consumes it. (Mirror
// of buildings.go advanceBaseQuest.)
func (m *Module) advanceBaseQuest(tick int64) {
	b := m.wd.base()
	if b == nil {
		return
	}
	if b.level < 1 {
		b.level = 1
	}
	// Announce the initial quest once per (re)start so a subscribed controller
	// learns the goal even before the first level-up.
	if !m.questAnnounced {
		m.questAnnounced = true
		reqOre, reqMetal := m.cfg.questFor(b.level)
		m.emit(EventQuestUpdated, b.id, tick, map[string]any{"level": b.level, "requirements": map[string]any{"ore": reqOre, "metal": reqMetal}})
		m.feedAdd(FeedEvent{Kind: EventQuestUpdated})
	}
	// Level up while the store can pay the current quest (drops are capped at the
	// requirement, so the store never exceeds it): reset to 0, not subtract.
	for {
		reqOre, reqMetal := m.cfg.questFor(b.level)
		if b.ore < reqOre || b.metal < reqMetal {
			break
		}
		b.ore = 0
		b.metal = 0
		b.level++
		nextOre, nextMetal := m.cfg.questFor(b.level)
		m.emit(EventBaseLevelUp, b.id, tick, map[string]any{"level": b.level, "quest": map[string]any{"ore": nextOre, "metal": nextMetal}})
		m.feedAdd(FeedEvent{Kind: EventBaseLevelUp, Amount: b.level})
		m.emit(EventQuestUpdated, b.id, tick, map[string]any{"level": b.level, "requirements": map[string]any{"ore": nextOre, "metal": nextMetal}})
		m.feedAdd(FeedEvent{Kind: EventQuestUpdated})
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
		// A production store: robots drop ore/metal here to fuel robot building,
		// and land here to charge via the `charge` command.
		nb.hasStorage = true
		nb.cap = m.cfg.StationStorageCap
	}

	wd.removeBuilding(plat.id)
	wd.addBuilding(nb)

	m.emit(EventConstructionComplete, "", tick, map[string]any{"building_id": newID, "type": typ})
	m.feedAdd(FeedEvent{Kind: EventConstructionComplete})
}
