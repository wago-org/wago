# Agent Workflow

This project welcomes agent-assisted development, but the bar is the same as for
hand-written changes: small diffs, measured claims, reviewed code, and clear
ownership of correctness.

## Project Direction

Agents should steer `wago` toward a low-footprint, high-performance WebAssembly
JIT runtime written in Go. Favor changes that keep the engine fast, auditable,
and usable on small or memory-constrained machines.

## Commit Discipline

Use `skills/commit/SKILL.md` before preparing commits.

Commits should be atomic and usually fit one of three forms:

- add a red test for one missing behavior or regression;
- make the focused tests pass;
- combine red and green only when the behavior is too small to split usefully.

Each commit should carry the documentation needed by future developers or
agents. If the change affects workflow, tests, benchmarks, review expectations,
or agent behavior, update a file under `docs/` in the same commit.

## Measurement Expectations

For hot compiler, runtime, call-boundary, memory, or JIT-generated-code changes,
record the relevant proof:

- focused Go tests or wasm fixtures for correctness;
- benchmark results for speed-sensitive changes;
- allocation or memory-footprint notes when memory behavior changes;
- explicit notes when broader measurements were not run.

Avoid vague performance language. Prefer concrete before/after numbers or a
clear statement that the change is correctness-only.

## Review Focus

When reviewing agent-authored changes, inspect especially:

- validation rules and explicit rejection of unsupported wasm features;
- trap behavior and error paths;
- mmap, unsafe, stack, and native-call boundaries;
- allocations introduced in compile, instantiate, and execute hot paths;
- documentation updates that teach future agents the new workflow.
