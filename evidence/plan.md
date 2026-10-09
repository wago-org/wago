# Issue 815 local diagnostic experiment

Issue: https://github.com/wago-org/wago/issues/815
Verified main: 5cd443965a09e46076cce5e3dd4bf3ae69500348 (live REST ref and fetched Git agree).
Private repository/worktree; no shared worktree metadata or checkout changed.

Hypothesis (declared before implementation): a bounded, operand-preserving comparison detects changes hidden by opcode-only comparison (constants, zero-idiom dependency, GP widths, stack displacement), while explicit relocation-only changes compare equal. No optimization or runtime speedup is hypothesized.

Keep criteria: all prescribed paired controls distinguish meaningful changes; relocation-only controls compare equal only with matching explicit target identity; unsupported semantics stay unknown; size/count limits reject before decode/alignment; at least one current emitted fixture agrees with GNU objdump and compiler source maps; no production imports of tool. Linear positional alignment within matching source/function regions only; insertions or ambiguous anchors report inconclusive. Retain raw bytes. Reject/revise on a false equality or false semantic qualification. AMD64 native execution; ARM64 word decoding only, no native ARM64 qualification.

Budgets: <=1 MiB input JSON, <=64 regions, <=4096 instructions total, <=15 bytes per AMD64 instruction / exactly 4 bytes ARM64, <=512 changes. No unbounded sequence alignment or profiling campaign. Short measurements: 3 alternating baseline/candidate samples, each compile benchmark 100 iterations and execution benchmark 10000 iterations; GOMAXPROCS=2, build -p=2, then diagnostic benchmark 1000 iterations. No host/guest boundary changes. Monitor host load; ordinary source is identical so noisy timings are contextual controls only. Record B/op, allocs/op and native bytes; RSS is unmeasured. Diagnostic-only retained payload bounded by limits.

Inspection: 12 open issues, 32 open PRs; avoid 909/910/895, RISC-V 284, paused SQLite/original experiments, and ownership of 810/812/813/819/826. 815 has no comments/assignees and no matching local/remote branch. 719 remains active: do not change its sidecars/profiler. This initial slice accepts explicit records from existing source-map/disassembly facilities; full integration and cross-target coverage remain deferred. Wago .agents and .codex are empty, no applicable AGENTS.md/SKILL.md/agent-todo found in Wago trees. Codex memory only reports historical installer context, not current experiment ownership. CONTRIBUTING.md and CONTEXT.md read. No dependency installation.
