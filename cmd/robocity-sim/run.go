package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// canonicalSeed is the module's canonical world seed — the map every city of this
// type starts from — used when no city (hence no seed) can be resolved.
const canonicalSeed = 7

// runOptions are the resolved `run` inputs.
type runOptions struct {
	target string // dir or main.go path (may be "")
	ticks  int
	seed   int // world seed; <0 means "unset" (resolve from the city, else canonical)
	json   bool
	quiet  bool
	city   string
	server string
}

// cmdRun materializes the local SDK, builds a temp go.work that overrides the
// published SDK with it, and runs `go run .` in the user's project against the REAL
// game engine (resolved + loaded by the SDK at runtime — downloaded/cached, or
// $SIMCODE_ENGINE_SO). A fresh run starts from tick 0 on the resolved seed.
func cmdRun(o runOptions) int {
	pkgDir, modRoot, err := resolveProject(o.target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}

	sdkDir, err := materializeSDK()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: preparing local SDK: %v\n", err)
		return 1
	}
	defer os.RemoveAll(sdkDir)

	workDir, err := os.MkdirTemp("", "robocity-work-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	defer os.RemoveAll(workDir)

	workFile := filepath.Join(workDir, "go.work")
	if err := writeGoWork(workFile, modRoot, sdkDir); err != nil {
		fmt.Fprintf(os.Stderr, "error: writing go.work: %v\n", err)
		return 1
	}

	// Resolve the world seed + a city label. An explicit --seed wins (offline, no
	// lookup). Otherwise borrow the seed from your city (repo -> slug -> public
	// snapshot seed), so the local map matches your live city's map. If that can't be
	// resolved (not in a repo, offline, no linked city), fall back to the canonical
	// map (seed 7) — a warning, never a hard failure.
	seed := int64(canonicalSeed)
	city := "local"
	switch {
	case o.seed >= 0:
		seed = int64(o.seed)
		if o.city != "" {
			city = o.city
		}
	default:
		slug := o.city
		if slug == "" {
			if repo := gitRepoSlug(pkgDir); repo != "" {
				if s, err := slugForRepo(o.server, repo); err == nil {
					slug = s
				}
			}
		}
		if slug != "" {
			if s, err := seedForCity(o.server, slug); err == nil {
				seed = s
				city = slug
			} else if !o.json {
				fmt.Fprintf(os.Stderr, "note: couldn't read '%s' seed (%v); using the canonical map (seed %d).\n", slug, err, canonicalSeed)
			}
		} else if !o.json {
			fmt.Fprintf(os.Stderr, "note: no city resolved; using the canonical map (seed %d). Pass --city/--seed to override.\n", canonicalSeed)
		}
	}

	env := append(os.Environ(),
		"GOWORK="+workFile,
		"ROBOCITY_SIM_TICKS="+strconv.Itoa(o.ticks),
		"ROBOCITY_SIM_CITY="+city,
		"ROBOCITY_SIM_SEED="+strconv.FormatInt(seed, 10),
		"CGO_ENABLED=1", // the engine loader is cgo (dlopen); force it on for `go run`
	)
	if o.json {
		env = append(env, "ROBOCITY_SIM_JSON=1")
	}
	if o.quiet {
		env = append(env, "ROBOCITY_SIM_QUIET=1")
	}
	if !o.json {
		fmt.Printf("[%s] running your code against the real engine (seed %d, %d ticks, fresh from tick 0)\n", city, seed, o.ticks)
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
// materialized local SDK. Because the SDK module path equals the published SDK
// path, the workspace `use` overrides the user's `require github.com/lyabah/
// simcode-sdk-go ...` with the local, engine-backed copy — no edit to the user's
// go.mod, and it resolves offline (readonly workspace mode).
func writeGoWork(path, modRoot, sdkDir string) error {
	content := fmt.Sprintf("go 1.23\n\nuse %q\nuse %q\n", modRoot, sdkDir)
	return os.WriteFile(path, []byte(content), 0o644)
}
