# Reduce guarded constant-store register pressure

Follow-up to the [correctness fix](../correctness/README.md), on the same isolated branch. The first fix held the constant in a register while computing its address, adding a spill in the pressure fixture. Its previously measured 2.83% slowdown was not statistically significant.

The AMD64 lowering now computes the address first, then obtains the constant register with the existing `intConstReadReg` helper, excluding the live address register from allocation. It retains cached-constant reuse and emits one eight-byte store. The pressure fixture now has zero spills instead of one, and its native code shrinks from 325 to 316 bytes. Six representative explicit-mode native sequences remain byte-for-byte identical. ARM64 retains the preceding correctness fix.

Full guard-enabled AMD64 runtime tests, backend/encoder tests, and targeted guard/SSE2/register-allocation checks pass. A diagnostic regression test covers three constant patterns under both feature masks: it fails with one spill on the previous lowering and passes with zero spills on the new lowering. The runtime tests retain full-memory byte oracles, every partial memory-end access length, large offsets, pressure, and effect/trap ordering. Native ARM64 execution remains unavailable.

## Fresh benchmark comparison

Ryzen 7 8845HS, Go 1.27.1, CPU 4 pinned, `GOMAXPROCS=1`. Seven rotating 200 ms samples compare the original split-store prototype (`split`), the first correctness fix (`early`), and the mitigation (`late`). The split form is timed only on valid accesses. Six cases in both bounds modes and two CPU profiles give 24 rows with three forms each.

| Guarded pressure-near-miss | Split | Early | Late | Late versus early | Late versus split |
| --- | ---: | ---: | ---: | ---: | ---: |
| Default features | 14,021 ns/op | 13,887 ns/op | 13,833 ns/op | −0.39%, p=0.710 | −1.34%, p=0.456 |
| Forced SSE2 | 14,238 ns/op | 15,592 ns/op | 13,585 ns/op | −12.87%, p=0.259 | −4.59%, p=0.209 |

The run was noisy, including large variation in unchanged controls. No timing difference between early and late materialization is statistically significant, and no significant pressure-case regression versus the split baseline was observed. These results do not establish recovery of an exact 2.83% runtime cost. The eliminated spill and smaller native sequence are deterministic improvements. All execution allocation medians remain zero; no real-workload or compilation-speed claim is made for this follow-up.

[summary.csv](summary.csv) contains all comparisons. Raw `split-*`, `early-*`, and `late-*` logs and the four `benchstat-*` outputs retain every sample and significance result. [mitigation.patch](mitigation.patch) applies on top of the correctness fix. Generated disassemblies and binaries live under the ignored `code/` directory and can be regenerated.

```bash
./experiments/signed-immediate-830/pressure/build-forms.sh
./experiments/signed-immediate-830/pressure/run-bench.sh
python3 experiments/signed-immediate-830/pressure/analyze.py > experiments/signed-immediate-830/pressure/summary.txt
go test -tags=wago_codegenstats,wago_regalloccheck ./src/core/compiler/backend/railshot/amd64 -run '^TestSignedImmediateStoreGuardPressure$' -count=1
```

The build script temporarily switches the two backend memory files and restores their starting contents on exit. Do not edit them concurrently. The analyzer requires seven samples per form; timing requires `benchstat` and `taskset` on PATH. The original optimization and correctness scripts also restore the mitigation after recreating their historical variants.
