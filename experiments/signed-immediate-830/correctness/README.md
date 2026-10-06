# Guard-page constant-store correctness fix

Requested follow-up to the [issue #830 experiment](../README.md), October 5, 2026. All changes remain in the isolated `codex/signed-immediate-830` worktree.

This report records the initial correctness fix. The current sources also include a subsequent [pressure mitigation](../pressure/README.md), which removes the additional spill described below by materializing the AMD64 constant after address calculation. The before/after measurements here are retained as historical evidence.

A later independent review found that one unaligned ARM64 `STR` alone does not establish a no-partial-write guarantee on every supported CPU. The current ARM64 lowering therefore requires the existing explicit full-access bounds proof for guarded i64 constant stores before writing. The historical patch and measurements below predate that additional check. A codegen regression covers 25 constant/offset combinations and rejects the earlier lowering's missing proof; narrow guarded stores retain their previous selection. Native ARM execution remains unverified locally. [Mozilla integer-store investigation](https://bugzilla.mozilla.org/show_bug.cgi?id=1666747#c2), [native hardware differences](https://bugzilla.mozilla.org/show_bug.cgi?id=1737225#c3).

## Correction

The old lowering emitted an i64 constant store as two dword stores. Explicit mode checked the complete eight-byte access first. Guard mode omitted that check, so a memory-end access could complete the first dword store before the second faulted. The guest trapped after memory had changed.

AMD64 now routes constants that do not fit a signed imm32 through the existing materialized-value store path in guard mode. This emits one eight-byte memory store. Constants that fit retain the experimental single qword immediate form. Explicit mode retains the original selection, including its checked split stores. ARM64 had the same split-store pattern; its guard-mode i64 constants now use its existing materialized-value path and a single eight-byte STR.

The reused path pins the value while computing the address and retains the existing pending-load handling and register cleanup. Neither correction adds an inline bounds check or changes narrower store widths. The [fix patch](fix.patch) applies on top of the pre-fix optimization prototype; it contains the AMD64 and ARM64 production changes only.

## Correctness verification

The original prototype's guard regression test still failed 18 subcases immediately before this fix. The expanded tests also reject the old code: 18 byte/trap subcases, one effects/reuse subcase, and three pressure subcases fail, plus their three parent tests. See [before-expanded-guard-tests.txt](before-expanded-guard-tests.txt).

With the fix:

- The full normal AMD64 runtime suite and the full guard-enabled runtime suite pass.
- Guard-enabled tests with register-allocation checks pass in default and forced SSE2 configurations.
- Backend disassembly tests verify a single qword store in guard mode for every tested eight-byte constant, with both explicit baseline and optional feature masks.
- Complete AMD64 backend/encoder, ARM64 encoder, and Wasm helper package tests pass.
- ARM64 runtime and backend test binaries cross-compile with guard pages and register-allocation checking. **Native ARM64 execution was unavailable**, so its runtime behavior has not been measured here.

The runtime oracles cover eleven constants, byte/word/dword/qword widths, six offsets including the signed and unsigned 32-bit boundaries, and every partial store length at the memory end. They compare the entire memory against independent little-endian bytes, verify the exact trap type, and verify that a failing guest store changes no bytes. Effects/reuse tests now run in both bounds modes. Pressure tests preserve seven live carriers and verify that completed earlier guest stores remain observable while the failing store makes no partial write; a later valid call also verifies that the instance remains usable.

Full runtime tests used the verified pinned Release 3 corpus copied into this worktree and the existing WABT 1.0.41 executable, as described in the parent report. No conformance baseline or original checkout was changed.

## Performance check

This compares the existing signed-immediate prototype **before versus after this correctness fix**, rather than comparing against the initial repository baseline. Ryzen 7 8845HS, CPU 4 pinned, Go 1.27.1, `GOMAXPROCS=1`, seven alternating 150 ms samples per form. Both binaries include `wago_guardpage`; the explicit-mode benchmarks are controls. There are 60 matched rows: 15 cases × two bounds modes × two CPU profiles. Every invocation's output and stored bytes are checked by the benchmark harness.

| Changed guard-mode case | Default time change | SSE2 time change | Native code bytes |
| --- | ---: | ---: | ---: |
| positive `0x80000000` | −4.64%, p=0.002 | −3.66%, p=0.023 | 224 → 200 |
| `MinInt32-1` | −5.38%, p=0.001 | −4.98%, p=0.001 | 224 → 220 |
| mixed halves | −5.73%, p=0.017 | −4.23%, p=0.001 | 232 → 228 |
| pressure, positive `0x80000000` | +2.83%, p=0.209 | +0.88%, p=0.365 | 348 → 325 |

All three changed low-pressure cases improve significantly. The changed pressure case has one additional spill in the diagnostic sequence (zero → one; zero reloads because the carrier is consumed through a memory-source add). Its measured slowdown is not significant. Signed-immediate pressure controls retain zero spills in both guard-mode diagnostic forms. All execution benchmarks remain zero B/op and zero allocations/op.

Across all 15 guard rows, execution geomeans are −0.46% default and −0.16% SSE2. Explicit-mode geomeans are +0.57% and +0.39%. All 15 explicit native sizes match, and six representative explicit-mode binaries are byte-for-byte identical to the original prototype. The nominal significant explicit-control regressions are default pressure +1.12% (p=0.038), default pressure-near-miss +1.45% (p=0.017), and SSE2 cache-line crossing +2.21% (p=0.011). They are retained rather than discarded; unchanged code does not establish a causal regression from this guard-only fix. There were no significant guard-mode regressions. These are unadjusted tests with seven samples on a desktop also used by other workers.

The original real-workload timings and compile timings were not repeated: the AMD64 correction applies only to guard-mode emission, while the prior workload comparison used explicit bounds. This follow-up does not claim guard-mode real-workload speedups or native ARM64 performance. [summary.csv](summary.csv), raw logs, and `benchstat-*` files retain every individual result; the build script generates complete before/after sequences and spill counts under the ignored `code/` directory. [environment.txt](environment.txt) records the measured binary hashes.

## Reproduce

Run from the worktree root. The build script changes the two backend memory files temporarily and restores their starting contents on exit. Do not concurrently edit them while building.

```bash
./experiments/signed-immediate-830/correctness/build-forms.sh
./experiments/signed-immediate-830/correctness/run-bench.sh
python3 experiments/signed-immediate-830/correctness/analyze.py > experiments/signed-immediate-830/correctness/summary.txt

go test -tags=wago_guardpage,wago_regalloccheck ./src/wago ./src/core/compiler/backend/railshot/amd64 -run '^TestSignedImmediateStore' -count=1
go test -tags=wago_guardpage,wago_regalloccheck,wago_amd64_sse2 ./src/wago ./src/core/compiler/backend/railshot/amd64 -run '^TestSignedImmediateStore' -count=1
go test -tags=wago_guardpage ./src/wago -count=1
```

The full runtime command requires the pinned conformance corpus and WABT on PATH. `benchstat` and `taskset` must be available for timing. The analyzer requires seven samples per form. The failing pre-fix binary can be run from `src/wago` with `../../.tmp/store-before-fix-modern.test -test.run='^TestSignedImmediateStore' -test.count=1`.

The correction and evidence accompany the optimization prototype and its tests.
