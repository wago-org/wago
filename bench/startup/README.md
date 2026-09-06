# Startup-latency sweep

Use this sweep to refresh the website's **Startup latency** data. It measures
the whole process path for one real binary per workload:

```text
exec() → load → compile → instantiate → run _start → exit
```

It compares interpreters and JITs with
[hyperfine](https://github.com/sharkdp/hyperfine). This directory creates the
data. `scripts/update-website-startup.mjs` creates the website section from that
data; `scripts/update-website-bench.mjs` does the same for performance data.

## Before You Run

Install Node.js, `hyperfine`, and the runtimes you want to measure. A missing
runtime binary is skipped. The sweep still writes results for the runtimes it
can find.

## Layout

- `runtimes.json` lists runtimes, their command shape, engine `tag`, and
  workloads. Each runtime's binary is `bin` on `PATH`. Set the named `env`
  variable to override it, such as `WASM3_BIN=/path/to/wasm3`.
- `twins/*.wasm` are committed work twins. Each runs its full workload from
  `_start`, so every CLI uses a plain `run`. The sweep needs only the runtimes,
  not a wasm toolchain.
- `src/*.rs` contains the Rust compute-twin sources. A `_start` wrapper is
  appended to the matching `bench/corpus/rust/*.rs` kernel. The `json-as` twin
  is AssemblyScript; see `skills/startup-latency-bench` for its build.
- `run.mjs` performs the sweep and writes `startup.json`.
- `startup.json` is the committed dataset consumed by the website generator.

## Run the Sweep

```sh
make bench-startup                 # → bench/startup/startup.json
# or point at specific binaries:
V8_BIN=… WASM3_BIN=… IWASM_BIN=… node bench/startup/run.mjs
```

Then regenerate the site from the saved data. This does not benchmark again:

```sh
make site                          # startup + performance + stats, then build
# or just the startup section:
make startup-website
```

## Method

The command is `hyperfine -N --warmup 5 --min-runs 30` with cold caches. Each
workload uses one hyperfine invocation with one named command per runtime. This
times every engine back to back under the same conditions.

The website sorts each workload from fastest to slowest. It scales bar widths
against the slowest non-LLVM runtime. Wavm's LLVM compilation is an outlier and
would otherwise flatten the other bars.

See `skills/startup-latency-bench/SKILL.md` for the twin construction, the
cold-cache gotchas per runtime, and how to attribute wago's own startup.
