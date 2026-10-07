# Startup-latency sweep

Use this sweep to refresh the website's **End-to-end latency** data. It measures
the whole process path for one real binary per workload:

```text
exec() → load → compile → instantiate → run _start → exit
```

It compares interpreters and compilers with
[hyperfine](https://github.com/sharkdp/hyperfine). This directory creates the
data. `scripts/update-website-startup.mjs` creates the website section from that
data; `scripts/update-website-bench.mjs` does the same for performance data.

## Before You Run

Install Node.js, `hyperfine`, and every runtime in `runtimes.json`. The sweep
requires the complete runtime set so a partial comparison cannot be published.
Wazy is measured through its compiler CLI; install the pinned release used by
the current captures with
`go install github.com/samyfodil/wazy/cmd/wazy@v0.3.0`.

## Layout

- `runtimes.json` lists runtimes, their command shape, engine `tag`, and
  workloads. Each runtime's binary is `bin` on `PATH`. Set the named `env`
  variable to override it, such as `WASM3_BIN=/path/to/wasm3`.
- `twins/*.wasm` are committed work twins. Each runs its full workload from
  `_start`, so every CLI uses a plain `run`. The sweep needs only the runtimes,
  not a wasm toolchain.
- `src/*.rs` contains the Rust compute-twin sources. A `_start` wrapper is
  appended to the matching `corpus/sources/rust/*.rs` kernel.
- `run.mjs` performs one host sweep and writes `startup-arm64.json` or
  `startup-amd64.json`.
- Both architecture-specific JSON files are committed inputs to the website
  generator. The generator requires matching source commits, workload order,
  runtime metadata, and result sets.

## Run the Sweep

```sh
just bench startup                 # → startup-<host-arch>.json
# or point at specific binaries:
V8_BIN=… WASM3_BIN=… WASMI_BIN=… WAVM_BIN=… node bench/startup/run.mjs
```

Then regenerate the site from the saved data. This does not benchmark again:

```sh
just site                          # startup + performance + stats, then build
# or just the startup section:
just site startup
```

## Method

The command is `hyperfine -N --warmup 5 --min-runs 30`. Each
workload uses one hyperfine invocation with one named command per runtime. This
times every engine back to back under the same conditions, with a fresh process
from spawn through exit for every run. The harness first runs every command once
as a correctness preflight.

The website sorts each workload from fastest to slowest. It scales bar widths
against the slowest runtime in that workload.

Keep the command and work twins unchanged when you compare results. Capture both
architectures from the exact same committed Wago revision. When you add or
rebuild a twin, document its source and build steps in this README.
