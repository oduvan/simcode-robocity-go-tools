# simcode-robocity-go-tools

The **local test tool** for the SimCode **Robot City Builder** game, for city
controllers written in **Go**. It lets you run your `main.go` on your machine and see
what your robots would do — **before** you push it to your city repo.

`robocity-sim run` drives your controller against the **real game engine**: the exact
same c-shared library the server runs, downloaded on demand and cached. There is **no
re-implementation** to drift and **no parity to maintain** — a local run is the
server's actual game logic.

> This is a **test tool**, not the platform and not your city repo. Your controller
> still ships by pushing to your city repo; this just lets you check it first.

## Install

```bash
go install github.com/oduvan/simcode-robocity-go-tools/cmd/robocity-sim@latest
```

or from a checkout:

```bash
git clone https://github.com/oduvan/simcode-robocity-go-tools
cd simcode-robocity-go-tools
go build -o robocity-sim ./cmd/robocity-sim
```

**Requirements:** the Go toolchain (1.23+) **plus `CGO_ENABLED=1` and a C compiler**
(`gcc`/`clang`). The engine is loaded over cgo (`dlopen`), so the tool — and the
`go run` it launches for your controller — must be built with cgo enabled (it is, by
default, whenever a C compiler is on your PATH). No third-party Go dependencies.

The first `run` downloads the engine for your OS/arch (a few MB) and caches it under
`~/.cache/simcode/`. The engine is **glibc**-linked, so run on a glibc host
(Linux/macOS; not musl/alpine).

## Run your controller

```bash
robocity-sim run                      # run ./ against the real engine (this repo's city seed)
robocity-sim run ./my-city            # a dir containing main.go + go.mod
robocity-sim run ./my-city/main.go    # or point at the file
robocity-sim run . --ticks 300        # shorter horizon
robocity-sim run . --json             # machine-readable (parse this)
robocity-sim run . --seed 7           # force a specific world seed
```

Run it **inside your city repo** and it auto-detects which city this is (via the git
remote) and borrows that city's **map seed** — so the local world matches your live
city's map — then runs a fresh simulation from tick 0. If it can't resolve a city (not
inside the repo, offline), it falls back to the **canonical map** (seed 7). Pass
`--seed`/`--city` to control this explicitly.

Options:

| Flag | Meaning |
| --- | --- |
| `--ticks N` | how many ticks to simulate (default 500) |
| `--seed S` | world seed (default: your city's seed, else the canonical map, 7) |
| `--json` | emit a JSON document (`{seed,ticks,city,summary,errors,feed}`) instead of text |
| `--quiet` | suppress the per-tick feed; print only the SUMMARY |
| `--city SLUG` | city slug whose map seed to borrow (default: auto-detected from the git remote) |
| `--server URL` | server base URL for engine download + seed lookup (default `https://robocity.lyabah.com`) |

Your `main.go` is used **unchanged**: it `import`s the published SDK
`github.com/lyabah/simcode-sdk-go`, registers `city.On(...)` handlers, and calls
`city.Run()` — the tool swaps the SDK for a local, engine-backed copy (see below) and
drives the tick loop for you.

## How it runs your unchanged `main.go`

Your `main.go` is `package main` and imports the **published** SDK — you can't import a
`package main`, so the tool **runs** it (`go run`) with the SDK swapped for a local,
engine-backed copy:

1. The CLI **materializes an embedded copy** of the local SDK (same public API as
   `github.com/lyabah/simcode-sdk-go`, but its runtime drives the **real engine** over
   cgo instead of Redis) into a temp dir, as a standalone module whose module path
   **equals** the published SDK path.
2. It writes a temporary **`go.work`** (via `GOWORK`) that `use`s both your project and
   that local SDK. Because the local module's path matches the published one, the
   workspace **overrides** your `require github.com/lyabah/simcode-sdk-go …` with the
   local copy — **without editing your `go.mod`**, and it resolves offline.
3. It runs `go run .` (with `CGO_ENABLED=1`) in your project. Your code compiles
   unchanged; `city.Run()` resolves + loads the engine `.so`, runs the local tick loop
   for N ticks, and prints the feed + SUMMARY (or JSON).

The SDK source is **embedded in the binary**, so this works the same whether you `go
install`ed the tool or cloned the repo.

## Read the output

The default output streams the per-tick **activity feed** (game events + your
`r.Log(...)` lines, tick-stamped) and ends with a **SUMMARY** (your scorecard): final
tick, robots alive/destroyed, buildings by type, Base level, ore/metal mined+stored,
spots found, discovered-cell count, and handler errors. `--json` gives the same as a
JSON document. The command **exits non-zero if any handler raised** — watch the exit
code / `handler_errors` in a loop.

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
  *current* live state — so it shows what your controller does from the beginning, not
  a continuation of your running city.
- **Crashes are surfaced, not swallowed.** If a handler panics, the run continues (one
  bad event can't kill the loop, like the server) but the tool reports it in the
  SUMMARY (`handler errors`) and via a non-zero exit code.

Set `SIMCODE_ENGINE_SO=/path/to/libengine-*.so` to run against a **local engine build**
instead of downloading (used by the smoke test and engine developers).
`SIMCODE_SERVER` overrides the download/lookup server.

## Inspect a live city without simulating

```bash
robocity-sim inspect             # this city's status         (public, no token)
robocity-sim inspect --state     # full current world state   (public, no token)
robocity-sim inspect --logs 100  # recent activity log lines  (needs SIMCODE_TOKEN)
robocity-sim inspect --list      # all your cities            (needs SIMCODE_TOKEN)
```

`inspect` and `--state` read the **public** city snapshot (no token). `--logs` and
`--list` use the authed MCP tools (`get_recent_logs` / `list_cities`) and need
`SIMCODE_TOKEN`.

## Examples

- [`examples/starter`](examples/starter) — the shipped Go template (explore only), a
  verbatim copy of `templates/go-starter/main.go`.
- [`examples/mine`](examples/mine) — places a mine and hauls its output home.

```bash
robocity-sim run examples/starter --ticks 300
robocity-sim run examples/mine    --ticks 1500 --quiet
```

## Test this tool (maintainers)

Per the platform's Docker-only rule (build/test inside `golang:1.26` with `gcc`; the
real-engine smoke test runs only with a local engine build via `SIMCODE_ENGINE_SO` —
without it, the CLI + materialize tests still run and the smoke tests self-skip):

```bash
docker run --rm -v "$PWD":/app -w /app golang:1.26 \
  sh -c "apt-get -qq update && apt-get -qq install -y gcc && \
         CGO_ENABLED=1 go build ./... && CGO_ENABLED=1 go test ./..."

# with a local engine build, the smoke test runs too:
docker run --rm -v "$PWD":/app -v /path/to/engine.so:/engine.so:ro -w /app golang:1.26 \
  sh -c "apt-get -qq update && apt-get -qq install -y gcc && \
         CGO_ENABLED=1 SIMCODE_ENGINE_SO=/engine.so go test ./..."
```

## License

MIT.
