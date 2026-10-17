# #918: Linux AMD64 GS addressing — eligibility evidence, boundary blocker

PR production baseline: branch parent `8097344a63a79e96230e09c883b2138232151370`. The diff from that parent adds only this record and the static survey test; it changes no production source. Historical timing samples below used `209e448c392510a0325d5b282a0d86a776fb379c` as their recorded baseline. Host: Linux 6.12, AMD Ryzen 7 8845HS. `/proc/self/auxv` reports `AT_HWCAP2=0x2`, so the kernel advertises enabled FSGSBASE instructions on this host. This does not establish Wago runtime safety or support on other hosts.

Focused static control: `GOCACHE=/tmp/wago-go-cache go test ./src/wago -run '^TestSegueScalarMemoryCoverage$' -count=1 -v`. It parses scalar memory opcodes (0x28–0x3e) in decoded Wasm bodies and checks the simple owned, unshared memory32 shape. Counts are **static sites**, not executed operations or precisely admitted native encodings.

| Module | Shape | Loads | Stores | Offsets > signed 32-bit |
| --- | --- | ---: | ---: | ---: |
| SQLite | owned memory32 | 48,675 | 27,828 | 0 |
| yyjson | owned memory32 | 2,736 | 2,344 | 0 |
| SHA-256 | owned memory32 | 22 | 28 | 0 |
| PHP | owned memory32 | 869,542 | 388,269 | 0 |
| MicroPython | owned memory32 | 7,610 | 6,755 | 0 |
| memory tree | owned memory32 | 1 | 1 | 0 |

The [Segue paper](https://shravanrn.com/pubs/seguecg.pdf) and [WABT integration](https://github.com/WebAssembly/wabt/blob/b835f1ce49e5803c597a0548099456d6be1d3d11/wasm2c/README.md#enabling-segue-a-linux-x86_64-target-specific-optimization) motivate this addressing form but do not measure this JIT. [Linux kernel documentation](https://www.kernel.org/doc/html/next/x86/x86_64/fsgs.html) says FSGSBASE enablement must be checked via HWCAP2 and that `ARCH_SET_GS` may be disabled. Wago's `src/core/runtime/trampoline_amd64.s` and native/host call paths do not currently save and restore GS around every guest transition, trap, host callback, cross-instance call, and reentry. No Go thread ownership contract exists for guest GS state. Introducing GS-relative native memory operations before proving and testing that transition contract could leave unrelated Go code running with guest GS state. The prototype is blocked at this safety boundary, not at CPU or kernel feature availability.

RBX remains required by runtime control fields at negative offsets from linear-memory base (`src/core/compiler/backend/railshot/amd64/memory.go`). Even a safe load/store conversion would not release that register in stage 2. Effective-address wrapping and offset/trap equivalence also remain unproved. This branch deliberately does not emit or run GS-relative guest code.

The focused test passes. Auxiliary-vector data are diagnostic: if `/proc/self/auxv` is absent or restricted, the test logs that the host feature is unknown and continues the static corpus scan. Production source is unchanged against the actual PR parent. No native-byte comparison against that parent was measured. Historical short boundary samples (`GOCACHE=/tmp/wago-go-cache go test ./src/core/runtime -run '^$' -bench '^(BenchmarkCrossBoundaryCall|BenchmarkHostCall)$' -benchtime=50ms -count=2 -benchmem`): cross-boundary historical baseline `209e448c` `21.18/20.69 ns`, historical experiment `21.38/20.87 ns`; host call historical baseline `110.9/113.6 ns`, historical experiment `112.8/113.7 ns`. The exact experiment binary identity is not retained in this record. All are `0 B/op`, `0 allocs/op`. Production source differs between `209e448c` and the actual PR parent, so these historical samples are not an unchanged-code control for this PR. They are short, noisy samples, **not GS transition measurements** or evidence of a PR speedup. Compile time, code size, dynamic access counts, execution, peak RSS, and transition costs for a GS variant are unavailable because no safe runnable variant exists.

Recommendation: the completed static survey is ready for review. A separate GS implementation requires a reviewed trampoline/thread-boundary design and focused trap/reentry stress oracle before altering memory emission. Do not enable GS addressing from this evidence.
