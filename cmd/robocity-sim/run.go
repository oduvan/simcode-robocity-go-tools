package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// canonicalSeed is the module's canonical world seed. It is only ever used when
// the caller ASKS for it (--canonical); it is not a fallback. See resolveWorld.
const canonicalSeed = 7

// Exit codes with distinct meanings, so a caller (or a script) can tell the
// failures apart instead of reading prose:
//
//	4 = the code would be REJECTED on deploy
//	5 = the acceptance rule could not be consulted ("I could not find out")
//	6 = the world you asked for could not be obtained
const (
	exitCodeRejected = 4
	exitCheckUnknown = 5
	exitWorldUnknown = 6
)

// runOptions are the resolved `run` inputs.
type runOptions struct {
	target    string // dir or main.go path (may be "")
	ticks     int
	seed      int  // world seed; <0 means "unset"
	canonical bool // run the module's canonical map, asked for explicitly
	fromLive  bool // resume the city's CURRENT state, asked for explicitly
	json      bool
	quiet     bool
	city      string
	server    string
	skipCheck bool
}

// resolvedWorld is the world a run will use, and where it came from.
type resolvedWorld struct {
	seed       int64
	city       string
	config     map[string]any
	moduleType string
	origin     string

	// Set only when resuming a running city (--from-live).
	resumed   bool
	mapState  json.RawMessage // the save envelope, handed to the engine unconverted
	store     json.RawMessage // the city-wide saved values, which live OUTSIDE the world
	prime     json.RawMessage // the display snapshot that seeds the read model
	saveTick  int64
	primeTick int64
	storeKeys int

	engineVersion       string
	engineVersionSource string
	serverEngineVersion string
}

// start describes where this run begins, for the banner and the summary.
func (w resolvedWorld) start() string {
	if w.resumed {
		return fmt.Sprintf("resumed at tick %d (saved world from tick %d)", w.saveTick+1, w.saveTick)
	}
	return "fresh world at tick 0"
}

// resolveCity is the city slug this run is about: --city, else the repo's linked
// city. It errors rather than guessing.
func resolveCity(o runOptions, pkgDir string) (string, error) {
	if o.city != "" {
		return o.city, nil
	}
	repo := gitRepoSlug(pkgDir)
	if repo == "" {
		return "", fmt.Errorf(
			"this directory is not a git repo with an 'origin' remote, so I cannot tell which city to run")
	}
	s, err := slugForRepo(o.server, repo)
	if err != nil {
		return "", fmt.Errorf("could not look up the city for %s on %s: %w", repo, o.server, err)
	}
	if s == "" {
		return "", fmt.Errorf("no city on %s is linked to %s", o.server, repo)
	}
	return s, nil
}

// resolveWorld picks exactly ONE world source and names it, or fails.
//
//	--seed N     an explicit seed you asked for
//	--canonical  the module's canonical map, asked for explicitly
//	--from-live  your city AS IT IS NOW: its saved world + saved store, continuing
//	             its own tick numbering — the situation a real push creates
//	(default)    your city's seed + per-city config, fresh from tick 0
//
// There is no fifth, implicit source. Two identical runs once produced two
// different worlds — different starting fleets, a different quest ladder —
// because a failed city lookup quietly became "the canonical map, seed 7", with
// exit code 0 and one changed word in the banner (#73 req 2). If the requested
// world cannot be obtained this returns an error and the run STOPS.
func resolveWorld(o runOptions, pkgDir string) (resolvedWorld, error) {
	if o.seed >= 0 {
		city := o.city
		if city == "" {
			city = "local"
		}
		return resolvedWorld{seed: int64(o.seed), city: city,
			origin: fmt.Sprintf("explicit --seed %d", o.seed)}, nil
	}
	if o.canonical {
		return resolvedWorld{seed: canonicalSeed, city: "local",
			origin: fmt.Sprintf("the module's canonical map (--canonical), seed %d", canonicalSeed)}, nil
	}

	slug, err := resolveCity(o, pkgDir)
	if err != nil {
		return resolvedWorld{}, err
	}

	if o.fromLive {
		doc, err := savedWorldOfCity(o.server, slug) // never falls back
		if err != nil {
			return resolvedWorld{}, err
		}
		// The read model must be seeded: a restored engine reports only the ticks
		// it runs, so without this the handlers would read a nearly empty world
		// while the engine held the full one. The display snapshot is the same
		// projection a live controller reads — but it is taken at the city's
		// CURRENT tick, so record the skew and let the banner state it.
		prime, primeTick, err := citySnapshotRaw(o.server, slug)
		if err != nil {
			return resolvedWorld{}, fmt.Errorf(
				"resumed city %q but could not read its display snapshot from %s to seed the "+
					"read model (%v). Refusing to run: your handlers would see an almost empty world",
				slug, o.server, err)
		}
		store := doc.Store
		if len(store) == 0 || string(store) == "null" {
			store = json.RawMessage(`{}`)
		}
		var storeMap map[string]json.RawMessage
		_ = json.Unmarshal(store, &storeMap)

		return resolvedWorld{
			seed: doc.Seed, city: slug, config: doc.Config, moduleType: doc.Type,
			origin:    fmt.Sprintf("city '%s' on %s, AS IT IS NOW (--from-live)", slug, o.server),
			resumed:   true,
			mapState:  doc.Save,
			store:     store,
			prime:     prime,
			saveTick:  doc.Tick,
			primeTick: primeTick,
			storeKeys: len(storeMap),

			engineVersion:       doc.EngineVersion,
			engineVersionSource: doc.EngineVersionSource,
			serverEngineVersion: doc.ServerEngineVersion,
		}, nil
	}

	seed, cfg, err := worldOfCity(o.server, slug)
	if err != nil {
		return resolvedWorld{}, err
	}
	return resolvedWorld{seed: seed, city: slug, config: cfg,
		origin: fmt.Sprintf("city '%s' on %s", slug, o.server)}, nil
}

// printWorldBanner states plainly which world this run uses (#73 req 2).
func printWorldBanner(w resolvedWorld, ticks int) {
	rule := strings.Repeat("=", 72)
	fmt.Println(rule)
	fmt.Printf(" WORLD : %s\n", w.origin)
	fmt.Printf(" seed  : %d\n", w.seed)
	if len(w.config) > 0 {
		keys := make([]string, 0, len(w.config))
		for k := range w.config {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%s", k, formatConfigValue(w.config[k])))
		}
		fmt.Printf(" config: %s\n", strings.Join(parts, ", "))
	} else {
		fmt.Println(" config: module defaults (this world carries no per-city config)")
	}
	fmt.Printf(" engine: robot-city   ticks: %d   start: %s\n", ticks, w.start())
	if w.resumed {
		fmt.Printf(" store : %d saved key(s) restored (robot memory starts empty, as it does "+
			"after a real push)\n", w.storeKeys)
		for _, line := range resumeCaveats(w) {
			fmt.Println(line)
		}
	}
	fmt.Println(rule)
}

// resumeCaveats are the two things a resumed run cannot promise. Said once,
// plainly. Silence is the dangerous option: a wrong engine restores a
// partly-zeroed world WITHOUT erroring, and a read model seeded from a newer
// display state disagrees with the restored world until the run catches up.
func resumeCaveats(w resolvedWorld) []string {
	var out []string

	serverVer := w.serverEngineVersion
	if serverVer == "" {
		serverVer = "unknown"
	}
	if w.engineVersionSource == "save" && w.engineVersion != "" {
		out = append(out, fmt.Sprintf(" engine check: save was produced by engine %s; "+
			"the server publishes %s", w.engineVersion, serverVer))
	} else {
		out = append(out,
			" engine check: NOT POSSIBLE — this save records no engine version, so I",
			fmt.Sprintf("               cannot verify it matches the engine you are running "+
				"(server publishes %s).", serverVer),
			"               A mismatched engine restores a partly-zeroed world WITHOUT any error.")
	}

	if skew := w.primeTick - w.saveTick; skew > 0 {
		out = append(out,
			fmt.Sprintf(" read model  : seeded from the city's display state at tick %d,", w.primeTick),
			fmt.Sprintf("               %d tick(s) NEWER than the saved world (tick %d) the engine", skew, w.saveTick),
			"               restored. The engine is authoritative; the two converge as the run proceeds.")
	}
	return out
}

// resumeBundle is the file the CLI hands to the client library for a --from-live run.
// The field names match the Python tool's run_local keyword arguments so the two
// languages describe the same three things.
type resumeBundle struct {
	Map      json.RawMessage `json:"map"`   // the save envelope, engine map state
	Store    json.RawMessage `json:"store"` // saved values, which live outside the world
	Prime    json.RawMessage `json:"prime"` // display snapshot seeding the read model
	SaveTick int64           `json:"save_tick"`
}

func writeResumeBundle(path string, w resolvedWorld) error {
	b, err := json.Marshal(resumeBundle{
		Map: w.mapState, Store: w.store, Prime: w.prime, SaveTick: w.saveTick,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// formatConfigValue renders a config value for the banner. JSON decodes every
// number as float64, so a seed would otherwise print as 1.0005725e+08 — unreadable
// exactly where the reader is checking WHICH world they got.
func formatConfigValue(v any) string {
	if f, ok := v.(float64); ok && f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return fmt.Sprintf("%v", v)
}

// checkCode asks the server whether this repo would be accepted on deploy.
// Returns 0 (accepted), exitCodeRejected, or exitCheckUnknown.
func checkCode(server, pkgDir string, quiet bool) int {
	files, err := collectSources(pkgDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: could not read %s: %v\n", pkgDir, err)
		return 2
	}
	verdict, err := validateSources(server, "go", files)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		fmt.Fprintln(os.Stderr,
			"       This run would not have told you whether a deploy accepts your code, so it stops here.\n"+
				"       Re-run when the server is reachable, or pass --skip-code-check to run anyway\n"+
				"       (a clean run then guarantees nothing about deploying).")
		return exitCheckUnknown
	}
	if !verdict.OK {
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "CODE-CHECK: REJECTED — a deploy would refuse this repo:")
		fmt.Fprintf(os.Stderr, "  %s\n", verdict.Error)
		fmt.Fprintln(os.Stderr, "  (this is the same rule the server runs on push; the release would never "+
			"load and your city would keep running the previous code)")
		return exitCodeRejected
	}
	if !quiet {
		fmt.Printf("CODE-CHECK: OK — %s would accept this repo (%d file(s)).\n", server, verdict.Files)
	}
	return 0
}

// cmdRun materializes the local client library, builds a temp go.work that overrides the
// client library with it, and runs `go run .` in the user's project against the REAL
// game engine (resolved + loaded by the client library at runtime — downloaded/cached, or
// $SIMCODE_ENGINE_SO). A fresh run starts from tick 0 on the resolved seed.
func cmdRun(o runOptions) int {
	pkgDir, modRoot, err := resolveProject(o.target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}

	// 1. Would a deploy accept this code at all? Same rule, asked of the server —
	//    never a second copy of it here (#73 req 3).
	if !o.skipCheck {
		if rc := checkCode(o.server, pkgDir, o.json); rc != 0 {
			return rc
		}
	}

	// 2. Which world? Stop rather than substitute (#73 req 2).
	world, err := resolveWorld(o, pkgDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		fmt.Fprintln(os.Stderr,
			"       I will not run a different world instead — the result would not be about your city.\n"+
				"       Choose one explicitly:\n"+
				"         robocity-sim run --city <slug>   run a specific city's world\n"+
				"         robocity-sim run --seed <N>      run a specific seed\n"+
				"         robocity-sim run --canonical     run the module's canonical map")
		return exitWorldUnknown
	}

	clientDir, err := materializeClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: preparing local client library: %v\n", err)
		return 1
	}
	defer os.RemoveAll(clientDir)

	workDir, err := os.MkdirTemp("", "robocity-work-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer os.RemoveAll(workDir)

	workFile := filepath.Join(workDir, "go.work")
	if err := writeGoWork(workFile, modRoot, clientDir); err != nil {
		fmt.Fprintf(os.Stderr, "error: writing go.work: %v\n", err)
		return 1
	}

	env := append(os.Environ(),
		"GOWORK="+workFile,
		"ROBOCITY_SIM_TICKS="+strconv.Itoa(o.ticks),
		"ROBOCITY_SIM_CITY="+world.city,
		"ROBOCITY_SIM_SEED="+strconv.FormatInt(world.seed, 10),
		"ROBOCITY_SIM_WORLD_ORIGIN="+world.origin,
		"ROBOCITY_SIM_WORLD_START="+world.start(),
		"CGO_ENABLED=1", // the engine loader is cgo (dlopen); force it on for `go run`
	)
	if world.moduleType != "" {
		env = append(env, "ROBOCITY_SIM_TYPE="+world.moduleType)
	}
	if len(world.config) > 0 {
		if b, err := json.Marshal(world.config); err == nil {
			env = append(env, "ROBOCITY_SIM_CONFIG="+string(b))
		}
	}
	if world.resumed {
		// Through a FILE, never an env var: a saved world is hundreds of kilobytes
		// and Linux caps a single env string at 128 KiB (MAX_ARG_STRLEN), so
		// passing it inline would fail with E2BIG on any real city.
		bundlePath := filepath.Join(workDir, "resume.json")
		if err := writeResumeBundle(bundlePath, world); err != nil {
			fmt.Fprintf(os.Stderr, "error: preparing the resumed world: %v\n", err)
			return 1
		}
		env = append(env, "ROBOCITY_SIM_RESUME="+bundlePath)
	}
	if o.json {
		env = append(env, "ROBOCITY_SIM_JSON=1")
	}
	if o.quiet {
		env = append(env, "ROBOCITY_SIM_QUIET=1")
	}
	if !o.json {
		printWorldBanner(world, o.ticks)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = pkgDir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		// The sim exited non-zero (e.g. 3 = the controller raised on some events;
		// it already printed its own diagnostics). Propagate the code rather than
		// masking it as a generic tool failure.
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "error: running controller: %v\n", err)
		return 1
	}
	return 0
}

// resolveProject turns the target (a dir, a main.go path, or "") into the package
// directory to run and the enclosing module root (dir holding go.mod).
func resolveProject(target string) (pkgDir, modRoot string, err error) {
	if target == "" {
		target = "."
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", "", fmt.Errorf("controller not found: %s", abs)
	}
	if info.IsDir() {
		pkgDir = abs
	} else {
		pkgDir = filepath.Dir(abs)
	}
	modRoot = findModuleRoot(pkgDir)
	if modRoot == "" {
		return "", "", fmt.Errorf("no go.mod found at or above %s (is this a Go city project?)", pkgDir)
	}
	return pkgDir, modRoot, nil
}

// findModuleRoot walks up from dir until it finds a go.mod, returning "" if none.
func findModuleRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// writeGoWork writes a temporary go.work that includes the user's module and the
// materialized local client library. Because the client library module path equals the client library
// path, the workspace `use` overrides the user's `require github.com/oduvan/
// simcode-go ...` with the local, engine-backed copy — no edit to the user's
// go.mod, and it resolves offline (readonly workspace mode).
func writeGoWork(path, modRoot, clientDir string) error {
	content := fmt.Sprintf("go 1.23\n\nuse %q\nuse %q\n", modRoot, clientDir)
	return os.WriteFile(path, []byte(content), 0o644)
}
