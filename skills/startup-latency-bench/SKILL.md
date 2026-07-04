# Startup Latency Bench Skill

Use this skill to reproduce the cross-runtime full-process startup-latency
comparison in `docs/startup-latency-2026-07.md`: time `exec()` → load →
compile → instantiate → execute → exit for one real-world binary across a mix
of interpreters and JITs.

> **Automated now:** the multi-workload sweep that feeds the website's Startup
> section is scripted in `bench/startup/` — `make bench-startup` writes
> `bench/startup/startup.json`, and `make site` regenerates the website from it
> (via `scripts/update-website-startup.mjs`). This skill documents the method
> and the twin construction behind that harness; read it when adding a workload
> or debugging a runtime's numbers.

## Method in one paragraph

One real binary (json-as SWAR, 22 KB, zero imports) runs its whole workload
from `_start`, so every runtime executes it with its plain `run` command and
hyperfine times the full process. A **noop twin** — identical exports, so the
full JSON code stays live and compile cost is unchanged, but `_start` is empty
— splits the total: noop ≈ startup (process + load + compile + instantiate),
workload − noop ≈ exec. A positive delta per runtime also proves `_start`
actually ran (guards against a CLI silently not invoking the entry).

## 1. Build the two modules

Sources live in the json-as checkout (`~/Code/AssemblyScript/json-as`), next
to the corpus bench entry `assembly/wago-bench.ts`. If missing, recreate:

`assembly/wago-startup.ts`:

```ts
import { serializeN, deserializeN } from "./wago-bench";

function wagoAbort(message: string | null = null, fileName: string | null = null, lineNumber: u32 = 0, columnNumber: u32 = 0): void {
  unreachable();
}

let sink: i32 = 0;

export function _start(): void {
  sink = serializeN(1000) + deserializeN(1000);
}

export function result(): i32 {
  return sink;
}
```

`assembly/wago-startup-noop.ts`:

```ts
export { serializeN, deserializeN } from "./wago-bench";

function wagoAbort(message: string | null = null, fileName: string | null = null, lineNumber: u32 = 0, columnNumber: u32 = 0): void {
  unreachable();
}

export function _start(): void {}
```

Build both (flags mirror `bench/corpus/build-as.sh` for json-as, minus
`--exportStart`, plus the abort rebind — `--use abort=<module>/<func>` is what
removes the `env.abort` import; a bare `--use abort=` breaks json-as's
explicit `abort()` calls):

```sh
cd ~/Code/AssemblyScript/json-as
JSON_MODE=SWAR node_modules/.bin/asc assembly/wago-startup.ts -o /tmp/json-startup.wasm \
  -O3 --noAssert --uncheckedBehavior always --disable simd --enable bulk-memory \
  --transform ./transform --runtime incremental --use abort=assembly/wago-startup/wagoAbort
JSON_MODE=SWAR node_modules/.bin/asc assembly/wago-startup-noop.ts -o /tmp/json-noop.wasm \
  -O3 --noAssert --uncheckedBehavior always --disable simd --enable bulk-memory \
  --transform ./transform --runtime incremental --use abort=assembly/wago-startup-noop/wagoAbort
```

Sanity: `wasm-tools print /tmp/json-startup.wasm | grep -c '(import'` must be 0.

## 2. Get the runtimes

- **wago**: `go build -o /tmp/bin/wago ./cli/wago` (matches `make build`).
- **wasmtime, wazero, wasmer, wavm**: upstream release binaries.
- **wasm3**: build from source; its FetchContent pins uvwasi to a dead
  `master` branch, so use the built-in WASI backend:
  `cmake -S wasm3 -B wasm3/build -DCMAKE_BUILD_TYPE=Release -DBUILD_WASI=simple`.
- **iwasm (WAMR)**: `cmake -S wasm-micro-runtime/product-mini/platforms/linux
  -B wamr-build -DCMAKE_BUILD_TYPE=Release` (default = fast interpreter).
- **wasmi**: `cargo install wasmi_cli`.
- **hyperfine**: musl binary from GitHub releases.

## 3. Measure

Run hyperfine once per module (`M=/tmp/json-startup.wasm`, then the noop):

```sh
hyperfine -N --warmup 5 --min-runs 30 --export-json out.json \
  -n wago              "/tmp/bin/wago run $M" \
  -n wasmtime          "wasmtime run -C cache=n $M" \
  -n wazero            "wazero run $M" \
  -n wasmer-cranelift  "wasmer run --disable-cache $M" \
  -n wasmer-singlepass "wasmer run --disable-cache -s $M" \
  -n wavm              "wavm run --abi=bare --function=_start $M" \
  -n wasm3             "wasm3 $M" \
  -n iwasm             "iwasm $M" \
  -n wasmi             "wasmi_cli $M" \
  -n wasmtime-warm     "wasmtime run $M" \
  -n wasmer-warm       "wasmer run $M"
```

Optionally add a `--version` matrix for per-binary process-spawn baselines.

## Gotchas (each one silently skews results)

- **wasmtime and wasmer cache compiled code by default.** Cold rows need
  `-C cache=n` / `--disable-cache`; the default-cache runs are a separate,
  legitimate "warm" row. Verify cold really recompiles: user time should far
  exceed wall time for wasmtime (parallel Cranelift).
- **wavm** refuses import-free modules as WASI commands; run with
  `--abi=bare --function=_start`.
- **hyperfine `-N`** (no shell) matters at millisecond scale.
- Use absolute paths in one command per runtime; every runtime except wavm
  picks up `_start` by itself (wago falls back to `_start`/`main`).
- Check every runtime's workload−noop delta is positive; ~0 is only plausible
  when compile time dwarfs exec (wavm, wasmer-cranelift).

## 4. Attribute wago's startup (in-process, no CLI noise)

```sh
cd bench && go test -run xxx -benchtime 200x \
  -bench 'BenchmarkDecode/json-as|BenchmarkValidate/json-as|BenchmarkCompile/json-as|BenchmarkInstantiate/json-as'
```

2026-07-03 reference (7800X3D): decode 76 µs · validate 0.6 ms · compile
2.0 ms · instantiate 18 µs — compile throughput is the startup story.
