// A small mining controller (Go) — the counterpart of the Python examples/
// mine_main.py. It exercises the build -> autonomous-mine -> haul -> Base-
// production path end to end, so `robocity-sim run examples/mine` shows a city
// that actually develops (buildings > 1, ore/metal mined climbing) rather than
// only exploring. It is deliberately simple, not an optimal city.
package main

import (
	"math"

	sc "github.com/lyabah/simcode-sdk-go"
)

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
		inv := r.Inventory()
		atBase := cx == bx && cy == by
		home := math.Abs(x-float64(bx)) + math.Abs(y-float64(by))

		// Energy guard: get home and charge before the battery runs dry.
		if r.Energy() <= home+15 {
			if atBase {
				r.Charge()
			} else {
				r.MoveTo(float64(bx), float64(by))
			}
			return
		}

		here := r.Here()

		// On a known ore spot with a free cell and a kit -> place a mine + seed it.
		if here.Spot != nil && here.Spot.Resource == "ore" && here.Building == nil {
			if inv.Ore >= 6 && inv.Metal >= 3 {
				city.World().Build(sc.BuildingMining, cx, cy)
				r.Drop()
				return
			}
		}

		// Standing on an active mine with output -> load up and haul it home.
		if here.Building != nil && here.Building.Type() == sc.BuildingMining &&
			here.Building.Status() == sc.StatusActive {
			if here.Building.Storage().Ore > 0 && !inv.IsFull() {
				r.PickUp()
				return
			}
		}

		// At the Base carrying ore -> drop it into the store (feeds robot production).
		if atBase && inv.Ore > 0 {
			r.Drop()
			// Once the Base has enough, grow the fleet.
			if base.Storage().Ore >= 12 && base.Storage().Metal >= 6 {
				base.BuildRobot(1)
			}
			return
		}

		// Head to the nearest known ore spot; otherwise explore to find one.
		if sx, sy, ok := nearestOreSpot(city, x, y); ok {
			if cx != sx || cy != sy {
				r.MoveTo(float64(sx), float64(sy))
				return
			}
		}
		if inv.Ore > 0 && !atBase {
			r.MoveTo(float64(bx), float64(by))
			return
		}
		// Explore outward to reveal new spots (flying reveals the map).
		r.Log("exploring for ore")
		r.MoveTo(x+5, y)
	})
	_ = city.Run()
}

// nearestOreSpot returns the closest discovered ore spot to (x,y) by Manhattan
// distance. No SDK nearest() in Go — iterate World().Spots() and pick the min.
func nearestOreSpot(city *sc.City, x, y float64) (int, int, bool) {
	best := math.MaxFloat64
	var bx, by int
	found := false
	for _, c := range city.World().Spots() {
		if c.Spot == nil || c.Spot.Resource != "ore" || c.Spot.Remaining <= 0 {
			continue
		}
		d := math.Abs(float64(c.X)-x) + math.Abs(float64(c.Y)-y)
		// Deterministic tie-break (World().Spots() is map-ordered): on equal
		// distance prefer the lower (x, then y) so the run is reproducible.
		if !found || d < best || (d == best && (c.X < bx || (c.X == bx && c.Y < by))) {
			best, bx, by, found = d, c.X, c.Y, true
		}
	}
	return bx, by, found
}
