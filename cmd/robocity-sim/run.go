package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// runOptions are the resolved `run` inputs.
type runOptions struct {
	target string // dir or main.go path (may be "")
	ticks  int
	json   bool
	quiet  bool
	city   string
	server string
}

// cmdRun materializes the local SDK, builds a temp go.work that overrides the
// published SDK with it, and runs `go run .` in the user's project against the
// in-process engine.
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

	// The tool always tests your code against your city's CURRENT state — "would
	// this work if I deployed it right now?". A city's live state is PUBLIC, so
	// this needs NO token: resolve the repo -> city slug and fetch the public
	// snapshot, then run your code against it.
	city := o.city
	if city == "" {
		repo := gitRepoSlug(pkgDir)
		if repo == "" {
			fmt.Fprintln(os.Stderr, "error: run this inside your city's git repo (so I can tell which city it is), or pass --city <slug>.")
			return 2
		}
		slug, err := slugForRepo(o.server, repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: couldn't reach %s: %v\n", o.server, err)
			return 1
		}
		if slug == "" {
			fmt.Fprintf(os.Stderr, "error: no city on %s is linked to %s. Create/link a city first, or pass --city <slug>.\n", o.server, repo)
			return 2
		}
		city = slug
	}

	snapPath, err := fetchLiveSnapshot(o.server, city, workDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: couldn't fetch '%s' state: %v\n", city, err)
		return 1
	}

	env := append(os.Environ(),
		"GOWORK="+workFile,
		"ROBOCITY_SIM_TICKS="+strconv.Itoa(o.ticks),
		"ROBOCITY_SIM_CITY="+city,
		"ROBOCITY_SIM_LIVE="+snapPath,
	)
	if o.json {
		env = append(env, "ROBOCITY_SIM_JSON=1")
	}
	if o.quiet {
		env = append(env, "ROBOCITY_SIM_QUIET=1")
	}
	if !o.json {
		fmt.Printf("[%s] testing your code against this city's CURRENT state\n", city)
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
