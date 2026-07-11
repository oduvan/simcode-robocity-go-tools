// SimCode city controller — a MINIMAL starting point.
//
// This starter does one thing on purpose: it keeps the robots alive and flies them
// around to explore the map. It does NOT mine, build, haul, or climb Base levels —
// that is for YOU to add.
//
// Read CLAUDE.md for the whole game (the goal, the buildings, the full SDK API) and
// grow this controller from here. The idea is simple: EventIdle fires whenever a robot
// needs its next order, so decide what the robot should do and issue one command.
package main

import (
	"math"

	sc "github.com/lyabah/simcode-sdk-go"
)

// Compass headings. A robot advances one heading per trip (kept in its memory) so the
// fleet fans out across the map instead of re-treading a single line into the fog.
var dirs = [8][2]int{{1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, -1}}

const (
	exploreHop   = 5  // world units to fly per exploration step
	chargeMargin = 15 // spare battery to keep on top of the trip home
)

var city *sc.City

func main() {
	city = sc.New()
	city.On(sc.EventIdle, onIdle)
	_ = city.Run()
}

func onIdle(e sc.Event) {
	r := city.Robot(e.Robot)
	x, y := r.Position()

	// Stay alive: a robot that runs its battery to zero mid-flight is destroyed, so head
	// back to the Base to recharge WHILE there's still enough energy to reach it. The Base
	// sits at the origin and doubles as a charging pad. (Distance-aware, not a fixed
	// threshold — otherwise a robot can wander further than it can fly back from.)
	if home := math.Hypot(x, y); r.Energy() < home+chargeMargin {
		if cx, cy := r.Cell(); cx == 0 && cy == 0 {
			r.Charge()
		} else {
			r.MoveTo(0, 0)
		}
		return
	}

	// Otherwise explore: fly a short hop along a rotating heading. Flying reveals the
	// map (~5 cells around the robot), so this is how you uncover resource spots.
	n := 0
	if v, ok := r.Memory()["hop"].(float64); ok {
		n = int(v)
	}
	n++
	r.SetMemory(map[string]any{"hop": n})
	d := dirs[n%len(dirs)]
	r.MoveTo(x+float64(d[0]*exploreHop), y+float64(d[1]*exploreHop))
}
