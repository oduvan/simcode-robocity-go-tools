# CLAUDE.md — using this test tool when writing city code

**This repo is a TEST TOOL, not a city.** It is the local test runner for the SimCode
**Robot City Builder** game, for controllers written in **Go**. If you are an AI
writing/iterating on a city controller (`main.go`), use this to **check your solution
locally BEFORE pushing** it to the city repo. It runs your `main.go` against the
**real** game engine — the exact same c-shared library the server runs, downloaded on
demand — so there is no re-implementation to drift and no network/GitHub/deploy wait.

## Install it

```bash
go install github.com/oduvan/simcode-robocity-go-tools/cmd/robocity-sim@latest
# or, from a checkout:  go build -o robocity-sim ./cmd/robocity-sim
```

**Needs the Go toolchain (1.23+) plus `CGO_ENABLED=1` and a C compiler** (`gcc`) — the
engine is loaded over cgo (`dlopen`). The first run downloads the engine for your
OS/arch (a few MB) and caches it under `~/.cache/simcode/`. No third-party Go deps. The
engine is glibc-linked (Linux/macOS; not musl/alpine).

## Run your controller

```bash
robocity-sim run main.go               # run against the real engine (uses THIS city's world)
robocity-sim run . --ticks 300         # shorter horizon
robocity-sim run . --json              # machine-readable (parse this)
robocity-sim run . --seed 7            # run a specific world seed
robocity-sim run . --from-live         # start from your city AS IT IS NOW (what a push meets)
robocity-sim run . --canonical         # run the canonical map (use this if you have no city yet)
robocity-sim check .                   # would a deploy ACCEPT this code? (no simulation)
```

Run it **inside your city repo** and it auto-detects which city this is (via the git
remote) and uses that city's **seed and per-city config** — so the local world matches
your live city — then runs a fresh simulation from tick 0.

### Start from your city as it is now

`--from-live` runs your controller forward from the city's **current state** instead of a
brand-new world: its saved world (buildings, fleet, stored materials, level, and every
robot's in-flight command, target and cargo) plus its **saved store**, continuing the
city's own tick numbering. That is the situation every deploy actually creates — new code
meeting a running city — and the one a cold start cannot reproduce.

A cold start stays the default: it is reproducible and it works before you have a city.

Both halves come along on purpose. The store lives OUTSIDE the world, so resuming the
world alone would give a city that looks right and behaves wrong: a controller that keeps
a claim registry or a version stamp there would start blank and re-do work the real city
has already done. Robot memory is left empty, which is exactly what a real push does.

If the live state cannot be obtained the run **stops** (exit `6`) — a city that has never
checkpointed yet is a refusal, not an empty world to invent.

A resumed run says two things plainly, because silence would be the dangerous option:

* **the engine cannot be verified** — nothing stamps a version into a save today, so the
  tool reports that the check is *not possible* and names the engine the server publishes.
  A mismatched engine restores a partly-zeroed world **without erroring**.
* **the read model is seeded from a slightly newer state** — after a restore the engine
  emits only incremental changes, so the read model your handlers see is seeded from the
  city's display snapshot, taken at the city's current tick. The banner reports the skew in
  ticks and the summary reports any drift between what your handlers saw and the counts the
  engine holds (the engine is authoritative).

### It never runs a world you did not ask for

If your city's world cannot be obtained (server unreachable, no city linked to this repo,
a snapshot with no seed), the run **stops** with exit code `6` and tells you how to
proceed. It does **not** quietly use a different world. That used to happen: a failed
lookup became "the canonical map, seed 7", so two identical runs minutes apart tested two
different worlds — different starting fleets, a different quest ladder.

To run a different world, ask for it: `--city <slug>`, `--seed <N>`, or `--canonical`.

Every run prints the world it used in a banner **and** repeats it in the SUMMARY next to
the verdict; `--json` carries a `world` block (`seed`, `city`, `origin`, `config`,
`start`) so an automated check can assert which world produced the numbers.

### It accepts exactly what a deploy accepts

Before simulating, `run` asks the server whether a real push would ACCEPT this repo,
using the same rule the server runs on push (`POST /api/code/validate`). This tool keeps
**no copy** of that rule — a copied allow-list is how a divergence between "passes
locally" and "refused on deploy" happens, and a refused release never loads while the
city silently keeps running the previous code.

Exit codes: `4` = a deploy would reject this code, `5` = the rule could not be consulted
("I could not find out" is not "accepted"), `6` = the world could not be obtained.
`--skip-code-check` opts out explicitly. `robocity-sim check` runs only this step.


`main.go` is used **unchanged**: it imports the client library
`github.com/oduvan/simcode-go`, registers `city.On(...)` handlers, and calls
`city.Run()`. The tool materializes a local, engine-backed copy of the client library (same public
API), overrides the published one with a temporary `go.work`, and runs `go run .` for
you — so your code compiles and runs unchanged, only the transport is swapped.

## Read the output

The run ends with a **SUMMARY** (your scorecard): the `world` it ran, `final tick`,
`robots` (alive), `robots expired`, `robots destroyed`, `buildings` (+ by type),
`base level`, ore/metal mined+stored, `spots found`, `discovered cells`, and
`handler errors`. `--json` gives the same as a JSON document. The command **exits
non-zero if any handler panicked** — watch the exit code / `handler_errors` in a loop.

### Expired is NOT destroyed
These are different things and are reported as different figures:

| figure | what happened | what it means |
| --- | --- | --- |
| `robots expired` | flew past its lifespan | **normal.** Inevitable end of life — build replacements. |
| `robots destroyed` | battery hit 0 mid-flight | **a bug in your code.** Cargo lost; recharge earlier / fly shorter hops. |

A long run turning over a hundred robots is a healthy fleet, not a fault. The
`LOCAL-CHECK` verdict keys on `robots destroyed` only.

### What "good" looks like
- `robots destroyed` should be **0** — a non-zero count means a robot ran its battery
  dry mid-flight (recharge earlier / fly shorter hops). `robots expired` may be any
  number; it is expected on a long run.
- Buildings growing (mining, storage, flying_station, station-produced robots) and the
  Base level climbing means the city is actually developing, not just exploring. The
  shipped starter only explores, so a fresh run shows `buildings: base=1, storage=1`
  and Base level 1 — beat that.

## It's the real engine (not a preview)

The game logic is the server's actual engine, so a local run is **not** an
approximation of the rules — same seed → same world, same mechanics, same event timing
(intents lag one tick, exactly like production). The only thing that differs from
production is the transport. Two caveats:

- A run starts from a **fresh tick-0 world** on your city's seed, not your city's
  *current* live state.
- **Crashes are surfaced, not swallowed.** If a handler panics, the run continues (one
  bad event can't kill the loop, like the server) but the tool reports it in the
  SUMMARY (`handler errors`) and via a non-zero exit code.

Set `SIMCODE_ENGINE_SO=/path/to/libengine-*.so` to run against a local engine build
instead of downloading (used by the smoke test + engine developers). `SIMCODE_SERVER`
overrides the download/lookup server.

## Inspect your city without simulating

```bash
robocity-sim inspect             # this city's status                     (public, no token)
robocity-sim inspect --state     # full current world state               (public, no token)
robocity-sim inspect --logs 100  # recent activity log lines              (public, no token)
robocity-sim inspect --errors    # unhandled exceptions since last release(public, no token)
robocity-sim inspect --errors --release all   # …across every release
```

`inspect` reads the server's **public REST API** — **no token, no MCP**: status/
`--state` from the city snapshot, `--logs` from `/logs`, `--errors` from
`/exceptions`. The city is auto-detected from this repo's git remote (or `--city`).
`--errors` groups exceptions by type + file:line, each with a sample traceback and
the log lines leading up to it — the first thing to check when a city looks
"frozen" (a raise leaves a robot uncommanded).

## Workflow for iterating on a city controller

1. Edit the city's `main.go`.
2. `robocity-sim run . --ticks 500 --json` and read the SUMMARY.
3. If robots stall (no growth), get destroyed, or nothing gets mined/built, adjust the
   strategy and re-run. It's deterministic — same seed reproduces the exact run.
4. Once it behaves, push `main.go` to the city repo.

## Repo layout (for maintainers of THIS tool)

- `localclient/` — the **local, engine-backed client library**: the same public API as the published
  `github.com/oduvan/simcode-go`, but its runtime drives the **real** engine over
  cgo instead of Redis. It is embedded (`embed.go`) and materialized at runtime.
  - `client.go` / `handles.go` / `contract.go` / `state.go` / `names.go` — the client library-facing
    read model + dispatch, copied verbatim from the client library (keep in sync).
  - `driver.go` — the tick loop: calls the real engine, applies each delta into the
    `mirror.go` WorldMirror, projects it into the read model, dispatches events, and
    feeds the produced intents back as next tick's commands. A Go port of the Python
    tool's `simcode/_local.py`.
  - `mirror.go` — the delta-applied world mirror (robots/buildings/tiles + discovered),
    projected into the state.* JSON the read model decodes.
  - `engine/` — the **cgo loader**: `dlopen`s the engine `.so` and exchanges JSON with
    its `EngineTick`/`EngineFree` C-ABI. This is the ONLY cgo package.
  - `enginedl/` — downloads + caches the engine `.so` from the server
    (`/api/engine/version` + `/api/engine/lib`); honors `SIMCODE_ENGINE_SO` /
    `SIMCODE_SERVER`. A Go port of `simcode/_engine_dl.py`.
- `cmd/robocity-sim/` — the thin CLI: `run` (materialize client library + go.work, resolve seed,
  `go run .`), `inspect` (public REST: snapshot / logs / exceptions), and the materialize /
  go.work plumbing.
- The engine itself is **not in this repo** — `run` downloads the real
  `libengine-robot-city-<os>-<arch>` and drives it. So there is **no parity to
  maintain**: a mechanics change on the server reaches this tool the moment the new
  engine is published, with no port needed here.

## Test this tool

Per the platform's Docker-only rule (build/test inside `golang:1.26` with `gcc`; the
real-engine smoke test runs only with a local engine build via `SIMCODE_ENGINE_SO`):

```bash
docker run --rm -v "$PWD":/app -w /app golang:1.26 \
  sh -c "apt-get -qq update && apt-get -qq install -y gcc && \
         CGO_ENABLED=1 go build ./... && CGO_ENABLED=1 go test ./..."
```
