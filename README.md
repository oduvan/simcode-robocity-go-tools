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
robocity-sim run                      # run ./ against the real engine (this repo's city world)
robocity-sim run ./my-city            # a dir containing main.go + go.mod
robocity-sim run ./my-city/main.go    # or point at the file
robocity-sim run . --ticks 300        # shorter horizon
robocity-sim run . --json             # machine-readable (parse this)
robocity-sim run . --seed 7           # run a specific world seed
robocity-sim run . --from-live        # run against your city exactly as it is right now
robocity-sim run . --canonical        # run the canonical map (use this if you have no city yet)
robocity-sim check .                  # would a deploy ACCEPT this code? (no simulation)
```

Run it **inside your city repo** and it auto-detects which city this is (via the git
remote) and uses that city's **seed and per-city config** — so the local world matches
your live city — then runs a fresh simulation from tick 0.

Options:

| Flag | Meaning |
| --- | --- |
| `--ticks N` | how many ticks to simulate (default 500) |
| `--seed S` | run this exact world seed instead of your city's |
| `--canonical` | run the module's canonical map instead of your city's world |
| `--from-live` | start from your city AS IT IS NOW (saved world + saved store) |
| `--json` | emit a JSON document (`{world,seed,ticks,city,summary,errors,feed}`) instead of text |
| `--quiet` | suppress the per-tick feed; print only the SUMMARY |
| `--city SLUG` | city slug whose world to run (default: auto-detected from the git remote) |
| `--skip-code-check` | don't ask the server whether a deploy would accept this code |
| `--server URL` | server base URL for engine download + world lookup (default `https://simgit.io`) |

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


Your `main.go` is used **unchanged**: it `import`s the client library
`github.com/oduvan/simcode-go`, registers `city.On(...)` handlers, and calls
`city.Run()` — the tool swaps the client library for a local, engine-backed copy (see below) and
drives the tick loop for you.

## How it runs your unchanged `main.go`

Your `main.go` is `package main` and imports the **published** client library — you can't import a
`package main`, so the tool **runs** it (`go run`) with the client library swapped for a local,
engine-backed copy:

1. The CLI **materializes an embedded copy** of the local client library (same public API as
   `github.com/oduvan/simcode-go`, but its runtime drives the **real engine** over
   cgo instead of Redis) into a temp dir, as a standalone module whose module path
   **equals** the client library path.
2. It writes a temporary **`go.work`** (via `GOWORK`) that `use`s both your project and
   that local client library. Because the local module's path matches the published one, the
   workspace **overrides** your `require github.com/oduvan/simcode-go …` with the
   local copy — **without editing your `go.mod`**, and it resolves offline.
3. It runs `go run .` (with `CGO_ENABLED=1`) in your project. Your code compiles
   unchanged; `city.Run()` resolves + loads the engine `.so`, runs the local tick loop
   for N ticks, and prints the feed + SUMMARY (or JSON).

The client library source is **embedded in the binary**, so this works the same whether you `go
install`ed the tool or cloned the repo.

## Read the output

The default output streams the per-tick **activity feed** (game events + your
`r.Log(...)` lines, tick-stamped) and ends with a **SUMMARY** (your scorecard): the
world it ran, final tick, robots alive, robots **expired**, robots **destroyed**,
buildings by type, Base level, ore/metal mined+stored, spots found, discovered-cell
count, and handler errors. `--json` gives the same as a JSON document. The command
**exits non-zero if any handler raised** — watch the exit code / `handler_errors`.

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
robocity-sim inspect             # this city's status                     (public, no token)
robocity-sim inspect --state     # full current world state               (public, no token)
robocity-sim inspect --logs 100  # recent activity log lines              (public, no token)
robocity-sim inspect --errors    # unhandled exceptions since last release(public, no token)
```

All of `inspect` reads the server's **public REST API** — **no token, no MCP**
(status/`--state` from the snapshot, `--logs` from `/logs`, `--errors` from
`/exceptions`). The city is auto-detected from this repo's git remote (or `--city`).
`--errors` is the first thing to check when a city looks "frozen" — a raised
handler leaves a robot uncommanded.

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
