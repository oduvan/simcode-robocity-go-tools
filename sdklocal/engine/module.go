package engine

// Module is the Robot City Builder rules engine. One Module drives one city.
// Lifecycle: New/NewWithConfig → ResetWorld → per tick Submit(intent)+Advance,
// with StateJSON()/DrainFeed() read by the driver.
type Module struct {
	cfg Config
	wd  *world

	evbuf []Event
	feed  []FeedEvent
	tick  int64
}

// New returns a Module with the default (provisional) tuning config.
func New() *Module { return NewWithConfig(DefaultConfig()) }

// NewWithConfig returns a Module with explicit tuning config.
func NewWithConfig(cfg Config) *Module { return &Module{cfg: cfg} }

// Config returns the module's tuning config (read-only copy).
func (m *Module) Config() Config { return m.cfg }

// ResetWorld initialises deterministic world state for city/seed.
func (m *Module) ResetWorld(city string, seed int64) {
	m.wd = newWorld(m.cfg)
	m.wd.generate(city, seed)
	m.feed = nil
}

func (m *Module) emit(name, robot string, tick int64, payload any) {
	m.evbuf = append(m.evbuf, ev(name, robot, tick, payload))
}

func (m *Module) feedAdd(f FeedEvent) {
	if f.Tick == 0 {
		f.Tick = m.tick
	}
	m.feed = append(m.feed, f)
}

// DrainFeed returns and clears the activity-feed entries accumulated this tick.
func (m *Module) DrainFeed() []FeedEvent {
	out := m.feed
	m.feed = nil
	return out
}

// Advance runs one tick: spawns, base production, mining, robot commands,
// constructions, idle notification. Returns events emitted this tick.
func (m *Module) Advance(tick int64) []Event {
	m.evbuf = nil
	m.tick = tick
	wd := m.wd

	m.emit(EventTick, "", tick, map[string]any{"tick_no": tick})

	for _, id := range wd.pendingSpawn {
		m.emit(EventSpawn, id, tick, nil)
	}
	wd.pendingSpawn = nil

	m.advanceProduction(tick)
	m.advanceMining(tick)

	for _, id := range append([]string(nil), wd.robotOrd...) {
		if r := wd.robots[id]; r != nil {
			m.advanceRobot(r, tick)
		}
	}

	m.advanceConstructions(tick)
	m.notifyIdle(tick)

	return m.evbuf
}

// notifyIdle emits `idle` for every robot with no active or queued command, once
// on the idle transition and then every IdleResendTicks while it stays idle.
func (m *Module) notifyIdle(tick int64) {
	resend := int64(m.cfg.IdleResendTicks)
	for _, id := range m.wd.robotOrd {
		r := m.wd.robots[id]
		if r.cmd != nil || len(r.queue) != 0 {
			continue
		}
		if r.idleEmittedTick == 0 || (resend > 0 && tick-r.idleEmittedTick >= resend) {
			r.idleEmittedTick = tick
			m.emit(EventIdle, r.id, tick, nil)
		}
	}
}

// Submit validates+registers an intent's commands, returning events emitted
// immediately.
func (m *Module) Submit(in Intent, tick int64) []Event {
	m.evbuf = nil
	m.tick = tick
	wd := m.wd

	for _, line := range in.Logs {
		m.feedAdd(FeedEvent{Kind: FeedKindLog, Robot: in.Robot, Text: line})
	}

	var robotCmds []Command
	for _, c := range in.Commands {
		switch c.Cmd {
		case CmdBuildRobot:
			if b := wd.base(); b != nil {
				n := argInt(c.Args, 0, 1)
				if n < 1 {
					n = 1
				}
				b.prodQueue += n
			}
		case CmdBaseCancel:
			if b := wd.base(); b != nil {
				b.prodQueue = 0
			}
		case CmdBuild:
			m.doBuild(c.Args, tick)
		default:
			robotCmds = append(robotCmds, c)
		}
	}

	if len(robotCmds) == 0 {
		return m.evbuf
	}
	r := wd.robots[in.Robot]
	if r == nil {
		return m.evbuf
	}

	if r.cmd != nil {
		m.emit(EventBlocked, r.id, tick, map[string]any{"reason": "interrupted"})
		m.feedAdd(FeedEvent{Kind: EventBlocked, Robot: r.id})
	}
	r.cmd = nil
	r.queue = nil
	r.idleEmittedTick = 0

	var cmds []*activeCmd
	for _, c := range robotCmds {
		if !allCommands[c.Cmd] {
			continue
		}
		cmds = append(cmds, &activeCmd{cmd: c.Cmd, args: c.Args})
	}
	if len(cmds) == 0 {
		r.state = StateIdle
		return m.evbuf
	}
	r.cmd = cmds[0]
	r.queue = cmds[1:]
	m.activate(r, tick)
	return m.evbuf
}

func (m *Module) activate(r *robot, tick int64) {
	for r.cmd != nil {
		if !m.beginCmd(r, tick) {
			return
		}
		m.popCmd(r)
	}
	r.state = StateIdle
}

func (m *Module) popCmd(r *robot) {
	r.cmd = nil
	if len(r.queue) > 0 {
		r.cmd = r.queue[0]
		r.queue = r.queue[1:]
	}
}

func (m *Module) finishCmd(r *robot, tick int64) {
	m.popCmd(r)
	if r.cmd != nil {
		m.activate(r, tick)
	} else {
		r.state = StateIdle
	}
}

func (m *Module) advanceRobot(r *robot, tick int64) {
	if r.cmd == nil {
		return
	}
	switch r.cmd.cmd {
	case CmdMoveTo:
		m.advanceMove(r, tick)
	case CmdCharge:
		m.advanceCharge(r, tick)
	}
}
