# CLAUDE.md — using this test tool when writing city code (Go)

**This repo is a TEST TOOL, not a city.** It is the local, offline simulator for
the SimCode **Robot City Builder** game, for controllers written in **Go**. If you
are an AI writing/iterating on a city controller (`main.go`), use this to **check
your solution locally BEFORE pushing** it to the city repo. It runs your `main.go`
against a faithful Go port of the server engine — no network, no GitHub, no Redis,
no waiting for a deploy.

## Install it

```bash
go install github.com/oduvan/simcode-robocity-go-tools/cmd/robocity-sim@latest
# or, from a checkout:
#   git clone https://github.com/oduvan/simcode-robocity-go-tools
#   go build -o robocity-sim ./cmd/robocity-sim
```

Needs the Go toolchain (1.23+) on PATH — the tool shells out to `go run` to
compile the controller.

## Run your controller

**By default it tests from your city's CURRENT state** — the whole point is
"if I push this *now*, does it do something sensible?". Run it **inside your city
repo** with your MCP token set; the tool auto-detects which city this repo is
(via the git remote), fetches that city's live state, and runs your new code
forward from there.

```bash
export SIMCODE_TOKEN=...   # your MCP token (dashboard → "Connect via MCP")

robocity-sim run .                # ← test from your city's CURRENT state (default)
robocity-sim run . --ticks 300    # shorter horizon
robocity-sim run . --json         # machine-readable (parse this)

robocity-sim run . --fresh        # clean seed-0 world (a brand-new city / a baseline)
robocity-sim run . --city other   # test against a specific city slug
```

The first output line tells you the mode: `[live] testing '<slug>' from its
CURRENT state` or `[fresh] seed 7, tick 0`. If auto-detect can't resolve a city
(no token, not in the repo, no linked city) it falls back to `[fresh]` and says
why. A live run is deliberately **approximate** — a quick "does it work now"
check, not a perfect sim; real edge cases surface after you push.

`main.go` is used **unchanged**: it does `import sc "github.com/lyabah/simcode-sdk-go"`,
registers `city.On(sc.EventIdle, …)` etc., and calls `city.Run()`. The tool
compiles it against a **local, engine-backed copy of the SDK** (swapped in via a
temporary `go.work`, without editing your `go.mod`), and `city.Run()` drives the
tick loop for you instead of talking to Redis.

## Read the output

- **Per-tick feed** (default): each line is `t<tick> <robot> <event>` for game
  events, or `t<tick> <robot>: <text>` for your `r.Log(...)` lines. This is your
  trace of what the fleet actually did.
- **SUMMARY** (always, at the end): `final tick`, `robots`, `robots destroyed`,
  `buildings` (+ by type), `ore`/`metal` **mined / stored**, `spots found`,
  `discovered cells`. This is your scorecard.
- `--json` gives `{seed, ticks, city, summary, feed[]}` — parse `summary` to grade
  a run and `feed` to see the sequence of events.

### What "good" looks like
- `robots destroyed` should be **0** — a non-zero count means a robot ran its
  battery dry mid-flight (recharge earlier / fly shorter hops).
- `ore.mined` / `metal.mined` climbing and `buildings_by_type` growing (mining,
  storage, flying_station, more base-produced robots) means the city is actually
  developing, not just exploring. The shipped starter only explores, so a fresh
  run of it shows `mined: 0` and `buildings: base=1` — beat that. See
  `examples/mine` for a controller that mines and hauls.

## Important: it's a faithful PREVIEW, not the server

- The engine here is a **re-implementation** of the server's Go engine (a
  DECOUPLED, in-process port under `sdklocal/engine`, with no Redis and no
  platform imports). World generation is **verified identical** (same seed → same
  map, spot positions and richness — `hashCell` is pinned against the Python port,
  which is byte-checked against the server), and the rules/events/timing mirror
  the server (intents lag one tick, just like production). Parity is maintained
  against the Go source; if you find a divergence in mechanics, treat it as a bug
  in this tool.
- The **default (live) mode** seeds from your city's current **public** snapshot,
  which is slightly lossy (fog-of-war hides undiscovered cells / spot richness, and
  in-flight command internals aren't carried). So a live run is an **approximate**
  continuation — perfect for "does my new code do something sensible right now",
  not a bit-exact forecast. `--fresh` is exact (deterministic seed-0 world) but is
  a *different* starting point than your running city.

## Handler errors & subscription fidelity

- **Panics are surfaced, not swallowed.** If a handler panics on an event, the
  run continues (one bad event can't kill the loop, exactly like the server) but
  the tool **reports it**: a `⚠ N handler error(s)` block on stderr, a
  `handler errors` line in the SUMMARY, an `errors[]` array in `--json`, and a
  **non-zero exit** (`go run` reports the sim's exit-3 as `exit status 3` and
  itself exits non-zero). So a bug in your controller shows up here instead of
  after a push. (Watch the exit code / the `handler_errors` count in a loop.)
- **Subscriptions behave like the server** for the normal pattern (handlers
  registered at import via `city.On(...)`), including idle re-emission (a passive
  handler keeps getting events; robots never permanently stall). The ONLY server
  behavior not reproduced: the *instantaneous replay* the server sends when a
  handler subscribes to `spawn`/`idle` **mid-run** — here that handler instead
  receives the next emission a few ticks later. Equivalent for virtually every
  controller.

## Workflow for iterating on a city controller

1. Edit the city's `main.go`.
2. `robocity-sim run ./my-city --ticks 500 --json` and read the `summary` + tail
   of `feed`.
3. If robots stall (no growth), get destroyed, or nothing gets mined/built, adjust
   the strategy and re-run. It's deterministic — same seed reproduces the exact
   run, so a change's effect is directly comparable.
4. Once it behaves, push `main.go` to the city repo.

## Repo layout (for maintainers of THIS tool)

- `sdklocal/` — the **local SDK**: the same public API as the published
  `github.com/lyabah/simcode-sdk-go`, but its runtime drives the in-process engine.
  - `names.go`, `contract.go`, `state.go`, `handles.go` — copied verbatim from the
    published SDK (the client API the user's code imports; do not change its shape).
  - `sdk.go` — the engine-backed `City` (New/On/Robot/Buildings/Base/World/Run).
  - `driver.go` — the tick loop (mirrors the Go engine `step`), feed + SUMMARY +
    `--json` output.
  - `engine/` — the ported rules engine: `world.go` (`hashCell`, endless lazy gen),
    `commands.go`/`buildings.go`/`module.go` (Submit/Advance, autonomous
    mining/construction, Base production, events), `state.go` (the state.* snapshot
    the SDK reads), `live.go` (`--from-live` seeding). Pure, deterministic, no Redis.
- `cmd/robocity-sim/` — the CLI: materializes the embedded SDK, writes the temp
  `go.work`, runs `go run` on the user's project, streams output. `--from-live`
  fetch is stdlib `net/http` only.
- `embed.go` — `//go:embed all:sdklocal`; the SDK source is baked into the binary
  so the tool works identically after `go install` or a `git clone`.
- `examples/` — `starter/` (the shipped template) and `mine/` (mines + hauls),
  each a standalone module (its own `go.mod`) that mimics a real user city repo.

Parity is guarded by porting the Go source under `game/modules/robot_city`. When
the Go engine changes, update `sdklocal/engine` and the copied client files in
`sdklocal/` together, and keep `hashCell` / config numbers in lockstep.

## Test this tool

Per the platform's Docker-only rule (image `golang:1.23-alpine`):

```bash
# Unit + integration tests (engine parity, determinism, flight/energy/destruction,
# autonomous mining, self-completing construction, Base production, SDK loop,
# CLI materialize/workspace):
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine go test ./...

# End-to-end: run the shipped starter through the CLI, fully offline (no Redis,
# no network). GOPROXY=off proves the workspace override needs no downloads.
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine sh -c '
  go build -o /usr/local/bin/robocity-sim ./cmd/robocity-sim &&
  cd examples/starter &&
  GOPROXY=off robocity-sim run . --ticks 300'
```

A good starter run shows 2 robots, robots moving, energy draining + recharging on
the Base, discovered cells growing, and **0** destroyed.
