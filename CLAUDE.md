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
robocity-sim run main.go               # run against the real engine (uses this city's map seed)
robocity-sim run . --ticks 300         # shorter horizon
robocity-sim run . --json              # machine-readable (parse this)
robocity-sim run . --seed 7            # force a specific world seed
```

Run it **inside your city repo** and it auto-detects which city this is (via the git
remote) and borrows that city's **map seed** — so the local world matches your live
city's map — then runs a fresh simulation from tick 0. If it can't resolve a city (not
inside the repo, offline), it falls back to the **canonical map** (seed 7). Pass
`--seed`/`--city` to control this explicitly.

`main.go` is used **unchanged**: it imports the published SDK
`github.com/lyabah/simcode-sdk-go`, registers `city.On(...)` handlers, and calls
`city.Run()`. The tool materializes a local, engine-backed copy of the SDK (same public
API), overrides the published one with a temporary `go.work`, and runs `go run .` for
you — so your code compiles and runs unchanged, only the transport is swapped.

## Read the output

The run ends with a **SUMMARY** (your scorecard): `final tick`, `robots`
(alive)/`robots destroyed`, `buildings` (+ by type), `base level`, ore/metal
mined+stored, `spots found`, `discovered cells`, and `handler errors`. `--json` gives
the same as a JSON document. The command **exits non-zero if any handler panicked** —
watch the exit code / `handler_errors` in a loop.

### What "good" looks like
- `robots destroyed` should be **0** — a non-zero count means a robot ran its battery
  dry mid-flight (recharge earlier / fly shorter hops).
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
robocity-sim inspect             # this city's status         (public, no token)
robocity-sim inspect --state     # full current world state   (public, no token)
robocity-sim inspect --logs 100  # recent activity log lines  (needs SIMCODE_TOKEN)
robocity-sim inspect --list      # all your cities            (needs SIMCODE_TOKEN)
```

`inspect` and `--state` read the **public** city snapshot (no token). `--logs` and
`--list` use the authed MCP tools (`get_recent_logs` / `list_cities`) and need
`SIMCODE_TOKEN`.

## Workflow for iterating on a city controller

1. Edit the city's `main.go`.
2. `robocity-sim run . --ticks 500 --json` and read the SUMMARY.
3. If robots stall (no growth), get destroyed, or nothing gets mined/built, adjust the
   strategy and re-run. It's deterministic — same seed reproduces the exact run.
4. Once it behaves, push `main.go` to the city repo.

## Repo layout (for maintainers of THIS tool)

- `sdklocal/` — the **local, engine-backed SDK**: the same public API as the published
  `github.com/lyabah/simcode-sdk-go`, but its runtime drives the **real** engine over
  cgo instead of Redis. It is embedded (`embed.go`) and materialized at runtime.
  - `sdk.go` / `handles.go` / `contract.go` / `state.go` / `names.go` — the SDK-facing
    read model + dispatch, copied verbatim from the published SDK (keep in sync).
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
- `cmd/robocity-sim/` — the thin CLI: `run` (materialize SDK + go.work, resolve seed,
  `go run .`), `inspect` (public snapshot / authed MCP tools), and the materialize /
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
