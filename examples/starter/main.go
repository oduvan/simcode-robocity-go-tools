// Robot City Builder — starter controller (Go). A verbatim copy of the shipped
// Go template (templates/go-starter/main.go). It is the end-to-end fixture for
// robocity-sim: `robocity-sim run examples/starter` runs THIS file, unchanged,
// against the in-process engine.
//
// The simplest thing that works: robots EXPLORE. Each one flies OUTWARD from the
// Base to reveal the map, then flies back to recharge before its battery runs out —
// and every trip it picks a NEW heading, so the fleet fans out across the whole
// area instead of re-treading one line. The Base doubles as a charging pad, so
// there's nothing to build — this is "hello, world".
package main

import sc "github.com/lyabah/simcode-sdk-go"

// Eight compass headings. A robot rotates through them (one per outbound trip) so
// successive trips sweep a fresh slice of the map — real exploration, not a shuttle.
var dirs = [8][2]int{{1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, -1}}

// Per-robot trip counter: how many outbound trips this robot has started. Bumped
// each time it leaves the Base, which advances its heading. Package state resets on
// a code reload — fine for exploring (the sweep just restarts).
var trip = map[string]int{}

func main() {
	city := sc.New()
	city.On(sc.EventIdle, func(e sc.Event) {
		r := city.Robot(e.Robot)
		base := city.Base()
		if base == nil {
			return
		}
		bx, by := base.Position()
		x, y := r.Position()
		cx, cy := r.Cell()
		home := abs(x-float64(bx)) + abs(y-float64(by)) // ~energy needed to fly home
		atBase := cx == bx && cy == by

		// Low battery: fly home and charge while we still can get there.
		if r.Energy() <= home+15 {
			if atBase {
				r.Charge()
			} else {
				r.Log("low battery — returning to base to charge")
				r.MoveTo(float64(bx), float64(by))
			}
			return
		}

		// Starting a fresh trip from the Base → advance the heading so this outing
		// explores new ground instead of repeating the last one.
		if atBase {
			trip[r.ID]++
			r.Log("charged — heading out to explore new ground")
		}
		d := dirs[(idSum(r.ID)+trip[r.ID])%len(dirs)]
		r.MoveTo(x+float64(d[0])*5, y+float64(d[1])*5)
	})
	_ = city.Run()
}

func idSum(id string) (n int) {
	for _, c := range []byte(id) {
		n += int(c)
	}
	return
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
