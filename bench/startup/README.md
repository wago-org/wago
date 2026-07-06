# Startup-latency sweep

Full-process, cross-runtime cold-start timing that feeds the website's
**Startup latency** section. Times `exec()` → load → compile → instantiate →
run `_start` → exit for one real binary per workload, across a mix of
interpreters and JITs, with [hyperfine](https://github.com/sharkdp/hyperfine).

This is the data half of the pipeline; the website half is
`scripts/update-website-startup.mjs` (analogous to `update-website-bench.mjs`
for the performance section).

## Layout

- `runtimes.json` — the runtime list (invocation + engine `tag`) and the
  workload list. Each runtime's binary is `bin` on `PATH`, overridable with the
  `env` var named there (e.g. `WASM3_BIN=/path/to/wasm3`).
- `twins/*.wasm` — committed **work twins**: each runs its whole workload from
  `_start` so every CLI executes it with a plain `run`. Checked in so the sweep
  needs no toolchain — only the runtimes.
- `src/*.rs` — sources for the Rust compute twins (a `_start` wrapper appended
  to the corresponding `bench/corpus/rust/*.rs` kernel). The `json-as` twin is
  AssemblyScript; see `skills/startup-latency-bench` for its build.
- `run.mjs` — the sweep. Skips any runtime whose binary isn't found and still
  writes the rest. Emits `startup.json`.
- `startup.json` — the dataset the website generator consumes (committed).

## Run it

```sh
make bench-startup                 # → bench/startup/startup.json
# or point at specific binaries:
V8_BIN=… WASM3_BIN=… IWASM_BIN=… node bench/startup/run.mjs
```

Then regenerate the site from the data (no benchmarking):

```sh
make site                          # startup + performance + stats, then build
# or just the startup section:
make startup-website
```

## Method

`hyperfine -N --warmup 5 --min-runs 30`, cold caches. Each workload is one
hyperfine invocation with one named command per runtime, so all engines are
timed back-to-back under identical conditions. The website panel sorts each
workload ascending and scales bar widths against the slowest non-LLVM runtime
(wavm's LLVM compile is an outlier that would otherwise flatten every bar).

See `skills/startup-latency-bench/SKILL.md` for the twin construction, the
cold-cache gotchas per runtime, and how to attribute wago's own startup.
