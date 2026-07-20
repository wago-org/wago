# AGENTS.md

## Direction

`wago` is a low-footprint, performance-oriented WebAssembly runtime in Go. Aim
for JIT-compiled wasm functions that run as fast as practical while still
working well on small, low-end, memory-constrained devices.

Prioritize:

1. correct wasm semantics;
2. small, predictable memory use;
3. fast hot paths for JIT code, host calls, memory, traps, and instantiation;
4. no-cgo operational simplicity; and
5. auditable compiler/runtime changes.

## Engineering Rules

- Decode, validate, and compile wasm features completely, or reject them clearly.
- Be strict, not fault-tolerant: a module that is malformed by spec — including a
  malformed `name` (or other structured) custom section — is rejected at decode,
  not silently ignored. This is intentional; do not "soften" such checks into
  best-effort parsing that accepts invalid input.
- Keep the JIT direct; add abstractions only when tests, benchmarks, or repeated
  code prove they are needed.
- Preserve the pure-Go runtime and firmware boundary: no cgo, C runtime, CMake,
  or mixed-language board shim. Small target assembly and linker scripts are
  acceptable where direct generated-code entry or fixed SRAM placement requires
  them.
- Avoid unbounded caches, goroutine-heavy designs, and hot-path allocations
  unless measured and justified.
- Make performance and footprint claims with numbers.
- Keep unsafe, mmap, stack, trap, and native-call boundaries boring and obvious.
- Check `FEATURES.md` and `ROADMAP.md` before changing feature support or
  priorities.

## Agent Workflow

- Read nearby code and tests before editing.
- Make the smallest coherent change and add/update tests with it.
- Run the most relevant tests; state what was not run.
- Include benchmark or memory notes for hot-path or footprint-sensitive changes.
- Update developer/agent docs in `docs/` when workflow, testing, benchmarking,
  review expectations, or agent behavior changes.
- Keep commits atomic; use `skills/commit/SKILL.md`.
