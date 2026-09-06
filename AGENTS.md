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

- Design for modularity and performance. Runtime performance is the highest
  priority after correctness, followed by a low memory footprint.
- Decode, validate, and compile wasm features completely, or reject them clearly.
- Be strict, not fault-tolerant: a module that is malformed by spec — including a
  malformed `name` (or other structured) custom section — is rejected at decode,
  not silently ignored. This is intentional; do not "soften" such checks into
  best-effort parsing that accepts invalid input.
- Keep the JIT direct; add abstractions only when tests, benchmarks, or repeated
  code prove they are needed.
- Measure compiler-related work as you do it. Treat operations that take longer
  than 30 seconds as performance bugs to investigate.
- Avoid unbounded caches, goroutine-heavy designs, and hot-path allocations
  unless measured and justified.
- Make performance and footprint claims with numbers.
- Keep unsafe, mmap, stack, trap, and native-call boundaries boring and obvious.
- Check `FEATURES.md` and `ROADMAP.md` before changing feature support or
  priorities.

## Agent Workflow

- Read nearby code and tests before editing.
- Use test-driven development when it is appropriate.
- Make the smallest coherent change and add/update tests with it.
- Adding tests that currently fail is acceptable; a failing suite is not itself
  a blocker. Never hide the failure.
- Do not write fail-closed tests. Failures must stay visible and diagnostic.
- Run the most relevant tests; state what was not run.
- Include benchmark or memory notes for hot-path or footprint-sensitive changes.
- Update and organize documentation with every commit. Keep it consistent with
  the implementation.
- Update the relevant developer or agent documentation when workflow, testing,
  benchmarking, review expectations, or agent behavior changes.
- Keep commits atomic, bounded, and easy to review; use
  `skills/commit/SKILL.md`.

## Done Means Done

Complete the whole requested task. Do not skip a part and call the task done.
If one part is genuinely blocked, complete the remaining parts and state the
specific blocker in one sentence.

## Act; Do Not Ask

For work that is cheap and reversible, act first and then report the result.
This includes research, data collection, analysis, drafts, in-scope refactors,
and API tests.

Ask first only before work that:

- reaches an audience;
- cannot be undone; or
- is expensive.

If something in scope is broken and can be fixed, fix it instead of only
reporting it.

## Questions Are Questions

When the user asks a question, answer it. Do not implement a change unless the
user asks for the change. When the request is ambiguous, answer it as a question
first.

## Responses

- Use short words, short sentences, and short paragraphs.
- Return only what is needed: what changed, whether it worked, and what the user
  needs to do next.
- If the user must choose, give at most two options, include only the context
  needed for a quick choice, and state the recommended option.
- Keep paths and commands exact.
- Use ASD-STE100 Simplified Technical English in user-facing responses.
