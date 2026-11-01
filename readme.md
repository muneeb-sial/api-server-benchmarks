# API Benchmarks

Hello-world HTTP servers in Rust, Go, Node, Deno, Bun, Python and Java, load-tested with
[k6](https://k6.io). Each one runs as a release/production build, inside a hard memory cap,
with no Docker. Every run produces an HTML report: RPS, p50/p95/p99/min/max latency, errors,
CPU and memory, as totals and over time.

There are two scenarios:

| Scenario | Request | Response |
|---|---|---|
| `text` | `GET /text` | `hello world` (`text/plain`) |
| `json` | `GET /json` | `{"message":"hello world"}` (`application/json`) |

k6 checks the status, body and content type of every response. A wrong answer counts as an error.

## Servers

```
rust/    axum, actix
go/      fiber, gin
node/    hono, express, fastify, nestjs-node, nestjs-express, nestjs-fastify
bun/     hono, express, fastify, nestjs-node, nestjs-express, nestjs-fastify
deno/    hono, express, fastify, nestjs-node, nestjs-express, nestjs-fastify
python/  fastapi, flask
java/    springboot
runner/  the Go orchestrator (bench.exe) and the k6 script
```

`nestjs-node` uses `@nestjs/platform-node`, the native `node:http` adapter added in Nest 12.3.

### Release builds and multi-core

Every server reads `PORT` and `WORKERS` from its environment.

| Runtime | Build | Uses cores via |
|---|---|---|
| Rust | `cargo build --release` (fat LTO, 1 codegen unit, `panic=abort`) | tokio / actix worker threads = `WORKERS` |
| Go | `go build -trimpath -ldflags="-s -w"` | `GOMAXPROCS=WORKERS` |
| Node | `npm install` (+ `tsc` for Nest), `NODE_ENV=production` | **pm2 cluster**: `WORKERS` processes via `pm2-runtime` |
| Bun | `bun build --minify` (Nest: `tsc`) | single process |
| Deno | `deno install` (Nest: `tsc`) | hono: `deno serve --parallel` (`DENO_JOBS=WORKERS`); others single process |
| Python | venv + pip | FastAPI: `uvicorn --workers WORKERS`; Flask: waitress with `WORKERS` threads |
| Java | `mvnw package` (Spring Boot 4, WebMVC/Tomcat) | JVM threads, `-XX:ActiveProcessorCount=WORKERS` |

Bun and Deno have no `node:cluster` / `SO_REUSEPORT` on Windows, so they run one process
(`maxWorkers: 1` in their `bench.json`). The report shows the worker count each result
actually used. Its **RPS per CPU core** column compares efficiency fairly across
single-process and multi-process servers.

## Prerequisites

k6, Go 1.24+, Rust, Node 22+, Bun, Deno 2, Python 3.10+ and Java 17+ must all be on `PATH`.
Maven isn't needed (the Maven wrapper is committed). The MSVC Rust toolchain also needs the
Visual Studio C++ build tools **and the Windows SDK** (Visual Studio Installer → Individual
components → Windows 11 SDK); without the SDK, cargo fails with "linker `link.exe` not found".

```powershell
go build -C runner -o ..\bench.exe .   # build the orchestrator
.\bench.exe doctor                      # check every toolchain is installed
```

## Usage

```powershell
.\bench.exe list                         # servers + effective memory / workers / CPUs
.\bench.exe build                        # build everything (or: -filter rust,go/gin)
.\bench.exe run                          # benchmark everything, both scenarios
.\bench.exe run -filter node -scenario json -duration 15s -vus 50
.\bench.exe report results\2026-11-01_12-00-00   # re-render a report
```

`-filter` takes comma-separated globs: `node` (the whole runtime), `node/nest*`, `rust/axum,go/*`.

Each run writes `results/<timestamp>/`:

```
summary.json                    every result, merged (the report reads this)
report.html                     the report: open it in a browser
<runtime>_<framework>/<scenario>/
  k6-summary.json               k6 end-of-test summary
  resources.csv                 CPU / memory samples of the server
  result.json                   this result, including both time series
  server.log, k6.log
```

`summary.json` is rewritten after every result, so a run you stop with Ctrl+C still gets a report.

## Configuration: `bench.config.yaml`

```yaml
defaults:
  memory_mb: 4096     # hard cap for the server's whole process tree
  workers: 6          # WORKERS
  server_cpus: "0-5"  # CPU affinity of the server
k6:
  vus: 100
  duration: 30s
  warmup: 10s
  cpus: "6-11"        # k6 runs on other cores, so it doesn't steal CPU from the server
servers:
  java/springboot: { memory_mb: 2048 }
  python/flask:    { workers: 12 }
  deno/express:    { disabled: true }
```

You can also switch `k6.executor` to `constant-arrival-rate` with a `rate` to measure latency at a
fixed load instead of at maximum throughput.

### How the memory cap works (no Docker)

The runner starts each server **suspended** and assigns it to a Windows **Job Object** with a
`JobMemoryLimit` before it runs a single instruction. It resumes the server only after that. Every
child process (pm2 cluster workers, uvicorn workers, …) inherits the job. So the cap applies to the
*whole tree*, and CPU and memory are measured for the whole tree. Killing the job leaves nothing
behind.

Runtimes don't see the job limit when they size their heaps, so the runner also passes hints:

| Variable | Value | Used by |
|---|---|---|
| `MEMORY_MB` | the cap | JVM `-XX:MaxRAM` |
| `HEAP_MB` | 80% of the cap | `GOMEMLIMIT`, Deno `--max-old-space-size` |
| `WORKER_HEAP_MB` | `HEAP_MB / WORKERS` | Node (pm2) and `deno serve --parallel` |
| `HEAP_BYTES` | `HEAP_MB` in bytes | Bun (`BUN_JSC_forceRAMSize`) |

You can use these in any `bench.json` as `${NAME}`, and they're also exported to the server's
environment. A server that reaches the cap gets status **Out of memory** in the report.

## Adding a server

Create `<runtime>/<framework>/` and put the server and a `bench.json` in it:

```json
{
  "build": ["cargo build --release"],
  "start": ["target/release/my-server.exe"],
  "env": { "SOME_VAR": "${WORKERS}" },
  "maxWorkers": 0,
  "notes": "shown in the report"
}
```

- `build` steps run through `cmd /C` in that folder.
- `start` is executed directly. A relative executable path is resolved against the folder.
- The server must answer `GET /text` and `GET /json` on `127.0.0.1:$PORT`.

`bench list` picks the new server up automatically.

## Caveats

- k6 and the server share one machine. CPU pinning (`server_cpus` / `k6.cpus`) keeps them on
  separate cores, but they still share memory bandwidth and the loopback stack.
- On Windows, k6 timers resolve to about 0.5 ms. For the fastest servers p50 may read 0, so
  compare those servers on RPS, p99 and max.
- A result marked **Stalled** means the server stopped answering for 2 or more seconds while k6
  was still sending requests. k6 counts those hung requests as interrupted rather than failed,
  so the runner checks the per-second RPS itself. `deno serve --parallel` did this once in 5 runs
  during testing; re-run the server to confirm.
- Only Windows is implemented. A Linux backend would use cgroup v2 in `runner/internal/proc`.
