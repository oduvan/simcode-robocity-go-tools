package simcode

import (
	"strings"
	"testing"
)

// A panicking handler must be isolated (not kill the run) like the server, but
// CAPTURED so the tool can report the bug locally instead of swallowing it.
func TestHandlerPanicIsCapturedNotSwallowed(t *testing.T) {
	t.Setenv("ROBOCITY_SIM_TICKS", "5")
	t.Setenv("ROBOCITY_SIM_CITY", "unit")
	c := New()
	c.On(EventIdle, func(e Event) { panic("boom") })

	// dispatch directly (Run() would os.Exit on captured errors).
	c.dispatch(Event{Event: EventIdle, Robot: "r1"})

	if len(c.errors) != 1 {
		t.Fatalf("want 1 captured handler error, got %d", len(c.errors))
	}
	if c.errors[0].Event != EventIdle || c.errors[0].Robot != "r1" {
		t.Fatalf("captured error has wrong context: %+v", c.errors[0])
	}
	if !strings.Contains(c.errors[0].Err, "boom") {
		t.Fatalf("captured error missing panic message: %q", c.errors[0].Err)
	}
}
