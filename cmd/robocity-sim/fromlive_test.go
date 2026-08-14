package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #73 req 1: run from the city's CURRENT state, not only from a fresh world.
//
// A push never starts a new world. It loads new code into a running, mature city
// where in-memory values are reset, saved values survive, and robots are part-way
// through journeys carrying cargo. --from-live reproduces exactly that.

const saveBody = `{"slug":"my-city","saved":true,"type":"robot-city",
  "seed":100057250,"config":{"starting_fleet":"3"},
  "tick":14460,"seq":14460,
  "engine_version":"","engine_version_source":"none","server_engine_version":"e191ef9",
  "save":{"tick":14460,"seq":14460,"world":{"seed":100057250}},
  "store":{"claims":{"r1":"mining-4"},"version":"abc123"}}`

const snapBody = `{"tick":14562,"robots":[{"id":"r1"}],"buildings":[],"spots":[],"discovered":[]}`

// liveServer serves both reads a --from-live run needs. save="" ⇒ a 404 body.
func liveServer(t *testing.T, save, snap string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/save"):
			if save == "" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"slug":"my-city","saved":false,"reason":"no_save","error":"no save yet"}`))
				return
			}
			_, _ = w.Write([]byte(save))
		case strings.HasSuffix(r.URL.Path, "/snapshot"):
			if snap == "" {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(snap))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestFromLiveCarriesTheSaveTheStoreAndTheIdentity(t *testing.T) {
	srv := liveServer(t, saveBody, snapBody)
	defer srv.Close()

	w, err := resolveWorld(runOptions{seed: -1, fromLive: true, city: "my-city", server: srv.URL}, ".")
	if err != nil {
		t.Fatalf("from-live: %v", err)
	}
	if !w.resumed {
		t.Fatal("world should be marked resumed")
	}
	// The WHOLE save envelope goes to the engine, unconverted.
	var env map[string]any
	if err := json.Unmarshal(w.mapState, &env); err != nil {
		t.Fatalf("map state is not the save envelope: %v", err)
	}
	if env["tick"] != float64(14460) || env["world"] == nil {
		t.Fatalf("map state should be {tick,seq,world}, got %v", env)
	}
	if w.saveTick != 14460 || w.primeTick != 14562 {
		t.Fatalf("ticks: save=%d prime=%d", w.saveTick, w.primeTick)
	}
	// The store lives OUTSIDE the world and must come along.
	if w.storeKeys != 2 {
		t.Fatalf("store keys = %d, want 2", w.storeKeys)
	}
	if w.seed != 100057250 || w.moduleType != "robot-city" {
		t.Fatalf("identity: seed=%d type=%q", w.seed, w.moduleType)
	}
	if w.config["starting_fleet"] != "3" {
		t.Fatalf("per-city config not carried: %v", w.config)
	}
	if !strings.Contains(w.origin, "AS IT IS NOW") {
		t.Fatalf("origin should say it is the live state: %q", w.origin)
	}
	if !strings.Contains(w.start(), "resumed at tick 14461") {
		t.Fatalf("start should name the resumed tick: %q", w.start())
	}
}

// A city that has never checkpointed is NOT an empty world to invent.
func TestFromLiveWithNoSaveStops(t *testing.T) {
	srv := liveServer(t, "", snapBody)
	defer srv.Close()

	_, err := resolveWorld(runOptions{seed: -1, fromLive: true, city: "my-city", server: srv.URL}, ".")
	if err == nil {
		t.Fatal("no save must stop the run")
	}
	if !strings.Contains(err.Error(), "no saved world yet") || !strings.Contains(err.Error(), "--from-live") {
		t.Fatalf("the message should explain and point at the way forward: %v", err)
	}
}

func TestFromLiveWithUnreachableServerStops(t *testing.T) {
	_, err := resolveWorld(runOptions{seed: -1, fromLive: true, city: "my-city",
		server: "http://127.0.0.1:1"}, ".")
	if err == nil {
		t.Fatal("an unreachable server must stop the run")
	}
	if !strings.Contains(err.Error(), "could not reach") {
		t.Fatalf("unexpected message: %v", err)
	}
}

// Without a seeded read model the handlers would see an almost empty world — which
// would look like a working run. Refuse instead.
func TestFromLiveWithoutASnapshotStops(t *testing.T) {
	srv := liveServer(t, saveBody, "")
	defer srv.Close()

	_, err := resolveWorld(runOptions{seed: -1, fromLive: true, city: "my-city", server: srv.URL}, ".")
	if err == nil || !strings.Contains(err.Error(), "almost empty world") {
		t.Fatalf("want a refusal explaining the read model, got %v", err)
	}
}

func TestColdStartRemainsTheDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/save") {
			t.Error("the default run must not fetch a save")
		}
		_, _ = w.Write([]byte(`{"world":{"seed":100057250}}`))
	}))
	defer srv.Close()

	w, err := resolveWorld(runOptions{seed: -1, city: "my-city", server: srv.URL}, ".")
	if err != nil {
		t.Fatal(err)
	}
	if w.resumed || w.mapState != nil {
		t.Fatal("the default is a cold start")
	}
	if w.start() != "fresh world at tick 0" {
		t.Fatalf("start = %q", w.start())
	}
}

// The two things a resumed run cannot promise must be said, not implied.
func TestResumeCaveatsAreStated(t *testing.T) {
	srv := liveServer(t, saveBody, snapBody)
	defer srv.Close()
	w, _ := resolveWorld(runOptions{seed: -1, fromLive: true, city: "my-city", server: srv.URL}, ".")

	out := strings.Join(resumeCaveats(w), "\n")
	if !strings.Contains(out, "engine check: NOT POSSIBLE") {
		t.Fatalf("an unverifiable engine must be reported: %s", out)
	}
	if !strings.Contains(out, "partly-zeroed world WITHOUT any error") {
		t.Fatalf("the consequence must be stated: %s", out)
	}
	if !strings.Contains(out, "102 tick(s) NEWER") {
		t.Fatalf("the read-model skew must be reported: %s", out)
	}
}

// A saved world is hundreds of kilobytes — far past Linux's 128 KiB cap on one env
// string — so it travels through a file.
func TestResumeBundleGoesThroughAFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resume.json")
	w := resolvedWorld{
		resumed: true, saveTick: 14460,
		mapState: json.RawMessage(`{"tick":14460,"world":{}}`),
		store:    json.RawMessage(`{"a":1}`),
		prime:    json.RawMessage(`{"tick":14562}`),
	}
	if err := writeResumeBundle(path, w); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var b resumeBundle
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	if b.SaveTick != 14460 || len(b.Map) == 0 || len(b.Store) == 0 || len(b.Prime) == 0 {
		t.Fatalf("bundle is incomplete: %+v", b)
	}
}

func TestWorldSourcesAreMutuallyExclusive(t *testing.T) {
	for _, args := range [][]string{
		{"--from-live", "--seed", "1"},
		{"--from-live", "--canonical"},
		{"--seed", "1", "--canonical"},
	} {
		if rc := runCmd(args); rc != 2 {
			t.Fatalf("%v should be a usage error, got %d", args, rc)
		}
	}
}
