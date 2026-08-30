# AMD64 optimization issue sweep

Date: 2026-08-29

Host: Linux/amd64, AMD Ryzen 7 8845HS, `GOMAXPROCS=1`. Final execution
watchpoints were pinned to CPU 2. Timed guest rows remain at 0 B/op and 0
allocations/op.

## Scope

The repository had 45 open issues, 34 of them labeled `performance` or
`area/performance`. This pass reviewed every one before changing code.

Most open performance issues are not collections of independent peepholes. They
are product or architecture projects that must remain separate PRs:

- runtime/footprint and artifact work: #305, #320, #329, #330, #331, #335;
- GC architecture: #308, #309, #313, #317, #321;
- compiler architecture and tooling: #316, #326, #327, #333, #399, #501, #503,
  #504, #506, #507, #508, #509, #511, #513;
- Component Model experiments: #434 and #435; and
- standards/proposals: #338, #339, #340, and #359.

Those issues need new representations, ABI work, proposal support, or profile
infrastructure before an execution A/B exists. Combining them into one PR would
violate the repository's atomicity and measurement rules.

The bounded candidates that can be switched or extended on current Railshot were
measured here. They cover the concrete #297 gaps and the local selection ideas in
#438, #501, #504, and #508.

## Baseline refresh for #297

The original issue recorded 22-23x backward-copy gaps, a 1.91x scalar BLAKE gap,
a 1.75x branch gap, and a 1.63x tiny-call gap. Current `main` had already removed
most of those gaps. The final retained branch measured:

| Workload | Wago median | wazero median | Wago/wazero |
|---|---:|---:|---:|
| tiny add | 8.599 ns | 26.050 ns | 0.330x |
| branch classifier | 8.586 ns | 24.550 ns | 0.350x |
| backward copy, 256 B | 1.142 us | 1.219 us | 0.937x |
| backward copy, 4 KiB | 11.170 us | 10.639 us | 1.050x |
| scalar BLAKE3 | 706.824 us | 475.709 us | 1.486x |
| SIMD BLAKE3 | 565.633 us | 530.466 us | 1.066x |

No listed row retains the severe gap recorded by the issue. Scalar BLAKE remains
the largest selector/allocator target, but it is now below 1.5x rather than 1.91x.

Command:

```sh
cd bench
GOMAXPROCS=1 go test -run '^$' \
  -bench '^(BenchmarkExec|BenchmarkWazeroExec)/(tiny\.add|branches\.classify|blake-as\.hashN|blake-as-simd\.hashN|isa_bulk_mem\.copy_back_256|isa_bulk_mem\.copy_back_4096)$' \
  -wago.bench.isa -benchmem -benchtime=300ms -count=5
```

## Retained: direct commutative self-updates

The existing default-off rule recognized:

```text
x = f(y) op x
```

for commutative `op`, but skipped the first site and implemented later sites by
swapping operands into the generic two-address path. The older August 22 broad
screen found that form neutral to negative and correctly defaulted it off.

The retained form changes two details:

1. admit the first safe site; and
2. pin the non-fixed destination, condense `f(y)` once, and consume it directly
   into that destination instead of entering generic RHS relocation and LHS
   sinking.

`RAX`, `RDX`, and `RCX` remain excluded because nested division, remainder, or
variable shifts can claim them. Non-commutative operations retain Wasm order.

### Execution

`BenchmarkExecCommuteSelfUpdate` compiles both products in one process against
real corpus payloads:

| Workload | Off median | On median | Change |
|---|---:|---:|---:|
| spectral norm | 685.189 us | 686.153 us | +0.14% |
| quicksort | 59.983 us | 59.413 us | -0.95% |
| JSON serialize | 24.732 us | 24.479 us | -1.02% |
| JSON deserialize | 44.592 us | 44.104 us | -1.09% |
| scalar BLAKE3 | 718.376 us | 703.297 us | -2.10% |
| open-coded multiply-high loop | 2.239 us | 1.995 us | -10.90% |
| SIMD BLAKE3 | 628.170 us | 575.795 us | -8.34% |

The seven-row geomean improves **3.55%**. Spectral norm's +0.14% is the worst
row and is noise-sized.

### Compile cost and native bytes

`BenchmarkCompileCommuteSelfUpdate` reports unchanged allocation counts and
bytes in all four retained compile rows:

| Module | Off | On | Time | Code bytes |
|---|---:|---:|---:|---:|
| scalar BLAKE3 | 571.087 us | 569.330 us | -0.31% | 11,469 -> 11,197 |
| SIMD BLAKE3 | 1.796 ms | 1.776 ms | -1.14% | 40,722 -> 40,482 |
| SQLite | 118.870 ms | 118.798 ms | -0.06% | 3,638,809 -> 3,630,329 |
| esbuild | 887.045 ms | 900.393 ms | +1.50% | 26,620,912 -> 26,619,504 |

The esbuild row is one-iteration and host-noisy; its allocation metrics are exact
and unchanged. Across the complete compile corpus, explain output records 2,556
retained self-update sites. Combined with the low-32 mask extension below:

- native bytes: **69,708,236 -> 69,675,415** (-32,821, -0.047%);
- allocator spills: **3,817 -> 1,259** (-2,558, -67.0%); and
- compile allocation traffic remains unchanged in the permanent watchpoints.

The rule now defaults on. `WAGO_AMD64_NO_COMMUTE_SELF_UPDATE=1` is the process
A/B and `WithOptimization("commute-self-update", false)` is the per-runtime A/B.

## Retained: all low-32 i64 masks

The existing #438-style clean-bit rule only selected a 32-bit `AND` for the
exact constant `0xffffffff`. The same proof applies to every constant in
`0..0xffffffff`:

```text
i64.and x, mask  =>  and r32, imm32
```

The destination write clears the upper 32 bits. For masks below `0x80000000`,
this removes `REX.W`; for masks with bit 31 set, it also avoids the temporary
register required by x86-64's sign-extended 64-bit immediate form. ZF, PF, CF,
and OF remain exact. SF can differ for bit-31 masks, but current ALU-result
consumers do not read SF; signed relations emit a separate compare.

`BenchmarkAMD64Low32MaskInstruction` is runtime-neutral within noise at about
0.45 ns/instruction and remains allocation-free. Focused module sizes are equal
or smaller; the full `0xffffffff` fixture falls 103 -> 99 bytes. The compile
corpus increases `i64-mask32` hits from 2,911 to 7,483 and removes another 4,800
native bytes beyond the old exact-mask implementation.

`WAGO_AMD64_NO_I64_MASK32=1` remains the rollback oracle.

## Measured candidates not retained

The following existing switches were measured against representative rows. None
cleared the default-on gate:

| Candidate | Focused result | Decision |
|---|---|---|
| float compare fusion | +1.41% geomean; raytrace +5.41% | keep off |
| vector local sinking | +2.04% geomean across SIMD JSON/BLAKE/UTF | keep off |
| call next-use + affine LEA | -0.52% geomean, but UTF +2.70% and mixed rows | keep off |
| loop precheck | -0.18% geomean; memory tree +1.64% | keep off |
| tee spill reuse | quicksort improved, but SHA/BLAKE rows regressed | keep off |
| BMI2 RORX | runtime tied while adding two bytes | keep experimental/off |

These results reinforce #501/#504's rule: fewer instructions or spills are not
sufficient. The same-process execution A/B remains the final gate.

## Issue disposition

- #297 can close: every named severe gap was remeasured and reduced to parity,
  a Wago win, or a sub-1.5x remaining scalar BLAKE gap.
- #438, #501, #504, and #508 receive measured progress, but remain open because
  their requested fact, selector, combiner, and regional-allocation scopes are
  broader than these two bounded rules.
- The other performance issues remain separate projects. No proposal, ABI,
  collector, artifact, component, or multi-pass compiler work was hidden inside
  this PR.
