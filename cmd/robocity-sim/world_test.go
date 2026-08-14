package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// #73 req 2: a run never silently substitutes a different world.
//
// Forum post 22: two identical invocations produced two different worlds — 3
// starting robots vs 2, a different quest ladder — because a failed city lookup
// quietly fell back to the canonical map (seed 7), exit code 0, one changed word
// in the banner.

func TestResolveWorldExplicitSeedIsNamedAsSuch(t *testing.T) {
	w, err := resolveWorld(runOptions{seed: 1234, server: "https://example.test"}, ".")
	if err != nil {
		t.Fatalf("explicit seed: %v", err)
	}
	if w.seed != 1234 || !strings.Contains(w.origin, "explicit --seed 1234") {
		t.Fatalf("got %+v", w)
	}
}

func TestResolveWorldCanonicalMustBeAskedForByName(t *testing.T) {
	w, err := resolveWorld(runOptions{seed: -1, canonical: true, server: "https://example.test"}, ".")
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	if w.seed != canonicalSeed || !strings.Contains(w.origin, "canonical") {
		t.Fatalf("got %+v", w)
	}
}

func TestResolveWorldUsesCitySeedAndConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/city/my-city/snapshot" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"world":{"seed":100057250,"config":{"starting_fleet":5}}}`))
	}))
	defer srv.Close()

	got, err := resolveWorld(runOptions{seed: -1, city: "my-city", server: srv.URL}, ".")
	if err != nil {
		t.Fatalf("city world: %v", err)
	}
	if got.seed != 100057250 {
		t.Fatalf("seed = %d, want the city's", got.seed)
	}
	// The config must ride along, or we run the city's MAP but not its WORLD.
	if got.config["starting_fleet"] != float64(5) {
		t.Fatalf("config not carried: %+v", got.config)
	}
	if !strings.Contains(got.origin, "my-city") {
		t.Fatalf("origin should name the city: %q", got.origin)
	}
}

// THE regression: the lookup fails and the answer is an error, not seed 7.
func TestResolveWorldStopsWhenTheCityCannotBeRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	got, err := resolveWorld(runOptions{seed: -1, city: "my-city", server: srv.URL}, ".")
	if err == nil {
		t.Fatalf("a failed lookup must stop the run, got world %+v", got)
	}
	if got.seed == canonicalSeed {
		t.Fatalf("it fell back to the canonical map instead of failing")
	}
}

func TestResolveWorldStopsWhenTheCityHasNoSeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"world":{}}`))
	}))
	defer srv.Close()

	if _, err := resolveWorld(runOptions{seed: -1, city: "c", server: srv.URL}, "."); err == nil {
		t.Fatal("a snapshot with no seed must stop the run")
	}
}

func TestResolveWorldStopsWhenNoCityIsLinked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil) // city-by-repo 404 → "" slug
	}))
	defer srv.Close()

	// A directory with no git remote cannot name a city either.
	if _, err := resolveWorld(runOptions{seed: -1, server: srv.URL}, t.TempDir()); err == nil {
		t.Fatal("no repo and no flags must stop the run rather than guess")
	}
}

func TestSeedAndCanonicalAreMutuallyExclusive(t *testing.T) {
	if rc := runCmd([]string{"--seed", "1", "--canonical"}); rc != 2 {
		t.Fatalf("naming two worlds should be a usage error, got %d", rc)
	}
}
