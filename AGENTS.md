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
- Keep the JIT direct; add abstractions only when tests, benchmarks, or repeated
  code prove they are needed.
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
