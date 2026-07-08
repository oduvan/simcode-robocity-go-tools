# simcode-robocity-go-tools

> ## ⚠️ DEPRECATED — superseded by the real shared engine
>
> Local testing no longer uses this re-implementation. The `simcode` Go SDK now drives
> your controller against the **real** game engine (the same stateless tick the server
> runs) via its local mode behind the `simcode_local` build tag. A polished one-command
> Go runner (`robocity-sim`) is still being packaged; in the meantime the fastest
> real-engine check is the Python local runner (`python -m simcode.local`, the engine
> and rules are identical).
>
> Because local testing now runs the actual engine (not a port), there is **no parity
> to maintain**, and this repo is **no longer kept in sync** with the server engine —
> its results may drift. The code is left here for reference.

A **local, offline simulator** for the SimCode **Robot City Builder** game, for
city controllers written in **Go**. It lets you test your `main.go` on your
machine — **no GitHub push, no Redis, no server** — and see what your robots
would do.

It runs your **unchanged** `main.go` (which imports the published SDK
`github.com/lyabah/simcode-sdk-go` and calls `city.Run()`) against a faithful,
in-process **Go port of the server engine**. World generation is a direct port
of the Go source and is **verified identical** (same seed 7 → same map, same spot
positions/richness — pinned against the Python port, which is byte-checked against
the server).

> This is a **test tool**, not the platform and not your city repo. Your
> controller still ships by pushing to your city repo; this just lets you check
> it first.

## How it runs your unchanged `main.go`

Your `main.go` is `package main` and imports the **published** SDK — you can't
import a `package main`, so the tool **runs** it (`go run`) with the SDK swapped
for a local, engine-backed copy:

1. The CLI **materializes an embedded copy** of the local SDK (same public API as
   `github.com/lyabah/simcode-sdk-go`, but its runtime drives the in-process
   engine instead of Redis) into a temp dir, as a standalone module whose module
   path **equals** the published SDK path.
2. It writes a temporary **`go.work`** (via the `GOWORK` env var) that `use`s both
   your project and that local SDK. Because the local module's path matches the
   published one, the workspace **overrides** your `require
   github.com/lyabah/simcode-sdk-go …` with the local copy — **without editing
   your `go.mod`**, and it resolves **offline**.
3. It runs `go run .` in your project. Your code compiles unchanged; `city.Run()`
   runs the local tick loop for N ticks and prints the feed + SUMMARY (or JSON).

The SDK source is **embedded in the binary**, so this works the same whether you
`go install`ed the tool or cloned the repo — no module-cache path juggling.

## Install

```bash
go install github.com/oduvan/simcode-robocity-go-tools/cmd/robocity-sim@latest
```

or from a checkout:

```bash
git clone https://github.com/oduvan/simcode-robocity-go-tools
cd simcode-robocity-go-tools
go run ./cmd/robocity-sim run /path/to/your-city   # or: go build -o robocity-sim ./cmd/robocity-sim
```

Needs the Go toolchain (1.23+) on your PATH — the tool shells out to `go run` to
compile your controller. No third-party dependencies; the CLI is stdlib-only.

## Usage

```bash
# Fresh canonical run (seed 7 — the same map every city of this type starts from):
robocity-sim run ./my-city            # dir containing main.go + go.mod
robocity-sim run ./my-city/main.go    # or point at the file
robocity-sim run                      # or just run in the project dir

# Shorter run, only the summary:
robocity-sim run . --ticks 200 --quiet

# Machine-readable output (for tooling / an AI reading the result):
robocity-sim run . --ticks 500 --json
```

Options:

| Flag | Meaning |
| --- | --- |
| `--ticks N` | how many ticks to simulate (default 500) |
| `--seed S` | world seed (default 7 — the canonical shared map) |
| `--json` | emit a JSON document (`{seed,ticks,city,summary,feed}`) instead of text |
| `--quiet` | suppress the per-tick feed; print only the summary |
| `--from-live` | seed the world from a live city (approximate preview) |
| `--city SLUG` | city slug (required with `--from-live`) / label |
| `--server URL` | MCP server base URL (default `https://robocity.lyabah.com`) |

The default output streams the per-tick **activity feed** (game events + your
`r.Log(...)` lines, tick-stamped) and ends with a **SUMMARY**: final tick, robot
count, buildings by type, ore/metal mined+stored, discovered-cell count, and how
many robots were destroyed.

### Preview from a live city (`--from-live`)

Seed the local run from a city's *current* world instead of a fresh start:

```bash
export SIMCODE_TOKEN=...        # your MCP bearer token
robocity-sim run . --from-live --city my-city-slug
# optional: --server https://robocity.lyabah.com  (default)
```

This fetches the city's public world snapshot over the MCP endpoint
(`POST {server}/mcp`, JSON-RPC `tools/call` → `get_world_state`, stdlib
`net/http`) and rebuilds an **approximate** world from it. Because the public
snapshot is a lossy, fog-limited view (no hidden spot richness, no in-flight
command internals), a `--from-live` run is a **rough preview** of "where my city
is now", not an exact continuation. If `SIMCODE_TOKEN` is unset you get a clear
error.

## What it models

Everything the reference module does, ported faithfully and deterministically:

- endless, continuous world with lazy **hash-based** cell generation (fog of war),
- **flying** robots with float positions and **energy** (drain on flight,
  destruction mid-flight when the battery hits 0, recharge on a Flying Station /
  the Base),
- **autonomous mining** into capped storage, **self-completing** construction
  sites (`World().Build`), and **Flying Station robot production**,
- the full event set (`spawn`, `idle`, `arrived`, `blocked`, `construction_*`,
  `resource_delivered`, `spot_depleted`, `storage_full`, `inventory_full`,
  `robot_produced`, `robot_destroyed`, `charge_complete`, `message`,
  `base_level_up`, `quest_updated`), delivered to
  your handlers exactly as on the server (intents lag one tick, same as prod).

## Determinism

Running the same controller with the same seed twice produces **identical**
output (event feed and summary). The engine has no wall-clock or RNG in its hot
path; ordered logic uses stable slices and sorted collections.

## Examples

- [`examples/starter`](examples/starter) — the shipped Go template (explore only).
- [`examples/mine`](examples/mine) — places a mine and hauls its output home,
  so the city actually develops (buildings > 1, ore mined climbing).

```bash
robocity-sim run examples/starter --ticks 300
robocity-sim run examples/mine    --ticks 1500 --quiet
```

## License

MIT.
