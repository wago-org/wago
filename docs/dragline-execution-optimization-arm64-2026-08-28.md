# Dragline application execution diagnosis — ARM64 — 2026-08-28

Dragline's implementation roadmap is present, but its performance plan is not
complete. The application execution gate still fails: after this optimization
slice, Dragline is 2.374x Railshot by geometric mean across the 30 executable
application modules on an Apple M4 Max. Lower is better.

This report supplements
`dragline-railshot-cranelift-corpus-arm64-2026-08-28.md`. The original report's
release-shaped execution measurements use four 100 ms samples per export. The
diagnostic corpus passes below use three alternating 50 ms samples per export;
the focused `matmul` checks use five alternating 100 ms samples. A release
decision requires refreshing the longer paired Dragline/Railshot/Cranelift run.

## Root cause

The minimized `matmul(64)` case initially measured 8.781x Railshot. Scaling the
matrix from 8 to 64 increased the ratio from 2.266x to 8.948x, locating the
problem in the hot nested loop rather than invocation or host-call overhead.

The initial Dragline body contained 1,587 ARM64 instructions and 777
stack-relative memory operations. Railshot emitted 638 instructions and 39
stack-relative operations. Dragline's structured emitter coupled two independent
decisions: it used registers for the operand stack only when every local also
fit in the local-register set. `matmul` has 21 locals but a shallow operand
stack, so every Wasm intermediate was unnecessarily written to and read from
the native frame.

After separating those decisions, the Dragline body has 1,276 instructions and
187 stack-relative operations. Remaining hot-loop debt is visible in repeated
explicit bounds checks, integer address construction, local-home traffic, and
floating-point values represented in GPR-backed operand slots.

## Implemented optimizations

1. Shallow scalar operand stacks now remain in registers even when the
   function has more locals than the full structured-register mode can hold.
   Existing local-register peepholes retain their stricter original gate.
2. Eligible scalar functions cache the linear-memory byte length in a reserved
   register. Calls and `memory.grow` disable the cache, so neither a callee nor
   the function can make the value stale.
3. Adjacent same-width floating-point binary operations reuse FP scratch
   registers. They remain two distinct instructions; using FMA would violate
   WebAssembly's separately rounded operation semantics.
4. Floating-point memory loops are admitted to RailMach when the rest of the
   function is supported. Rust `matmul` still uses the structured path because
   its setup includes `memory.fill` and saturating conversion, which RailMach
   does not yet finalize.

## Measured progression

| State | `matmul` Dragline | D/R ratio | 30-app D/R geometric mean | Native bytes |
|---|---:|---:|---:|---:|
| Original report / minimized reproduction | 1,345.7 us | 8.781x | 3.048x | 6,348 |
| Register-backed shallow operand stack | 437.1 us | 2.981x | 2.428x | 5,104 |
| Cached immutable memory length | 401.5 us | 2.684x | 2.403x | 4,952 |
| FP binary-pair scratch reuse | 390.5 us | 2.593x | 2.374x | 4,936 |

The focused `matmul` result is a 71.0% latency reduction and a 22.2% native-code
reduction from the minimized starting point. The application-suite ratio
improved by 22.1%, from 3.048x to 2.374x Railshot.

## Corpus campaign update — `blake-as` — 2026-08-29

The first corpus-by-corpus pass remeasured `blake-as.hashN(100)` on the current
branch before changing code. Five alternating 100 ms samples put Dragline at
661.6 us and Railshot at 387.4 us, or 1.708x Railshot. This is the relevant
baseline for the new allocator work; it supersedes the older 13.646x row below.

RailMach's greedy allocator previously ranked intervals by weighted live-range
area. In the 1,080-instruction BLAKE compression kernel, that score retained
long-lived, sparsely used state while repeatedly spilling short-lived values
used in the hot straight-line rounds. Large integer-only functions now rank
those intervals by squared weighted-use density. Smaller functions and
functions with FPR values retain the conservative area policy.

The final nine-round alternating 500 ms pass measured Dragline at 374.8 us and
Railshot at 380.0 us by median: 0.986x Railshot, or 1.4% faster. Relative to the
fresh pre-change Dragline baseline, execution latency fell 43.3%. The kernel's
native body moved from 7,256 to 5,108 bytes (-29.6%), ARM64 instructions from
1,808 to 1,270 (-29.8%), and stack-relative references from 693 to 162 (-76.6%).

Every one of the manifest's 36 executable exports completed with the new
policy. A three-round alternating 50 ms before/after diagnostic pass improved
their execution geometric mean by 2.1%; `blake-as-simd` also improved by 14.8%.
Those short suite-wide timings are prioritization evidence, not a replacement
for the release-shaped paired report.

## Corpus campaign update — `utf-as-simd` — 2026-08-29

The next corpus pass started with `convertN(200)` at 6.435x Railshot in the
three-round prioritization run. Its structured ARM64 body was 1,596 bytes and
contained 391 instructions, including 160 stack-relative references. The
scalar operand stack was disabled solely because the function also used v128,
even though v128 values already had a separate vector-register stack.

The mixed structured path now keeps up to four scalar operands in X9-X12,
pins eleven integer locals without consuming the cached memory-bound register,
materializes byte splats with `MOVI`, writes vector operations directly to their
stack destinations, and fuses `i8x16.bitmask` plus `i32.popcnt`. Pinned scalar
local arithmetic and comparisons bypass redundant operand-stack shuffles.
SIMD memory checks reuse the immutable memory length, and their exact Wasm trap
bodies are outlined after the normal return rather than occupying hot-loop
layout.

The progression below uses seven alternating 300 ms samples per backend on the
same Apple M4 Max. Values are medians.

| State | Dragline | Railshot | D/R | Function bytes |
|---|---:|---:|---:|---:|
| SIMD scalar stack + bitmask/popcnt + `MOVI` | 111.84 us | 55.54 us | 2.014x | 1,160 |
| Direct local and constant flow | 85.70 us | 55.34 us | 1.549x | 984 |
| Pinned locals + cached SIMD bounds | 77.98 us | 55.62 us | 1.402x | 936 |
| Direct vector destinations | 66.26 us | 55.65 us | 1.191x | 904 |
| Scalar update/reduction/branch folds | 58.86 us | 55.38 us | 1.063x | 768 |
| Cold SIMD memory traps | 45.92 us | 55.05 us | **0.834x** | 764 |

The final result is 16.6% faster than Railshot and 51.9% smaller than the
initial Dragline function. All 30 runnable and six compile-only corpus modules
remain admitted, and every executable export agrees with Railshot. A focused
regression also executes the fused bitmask/popcount result and verifies that an
out-of-bounds vector load reaches the outlined linear-memory trap.

A fresh three-round alternating 100 ms prioritization pass across all 36
executable exports puts the Dragline/Railshot execution geometric mean at
1.099x, down from 1.198x before this corpus slice. Dragline is faster on 19 of
36 exports. The remaining top four application gaps are now
`json-as-simd.deserializeN` (2.806x), `json-as-simd.serializeN` (2.533x),
`blake-as-simd.hashN` (1.989x), and `sha256.hashN` (1.558x).

`validateN(200)` is complete for this campaign slice. SIMD-only functions now
cache the immutable memory bound, and repeated vector constants and shuffle
masks remain in otherwise-unused vector registers. A direct fold for
`i8x16.bitmask != 0` avoids reconstructing a scalar bitmask when only its
nonzero state is observed. Expanding the mixed scalar operand stack from four
to six registers also removed avoidable frame traffic in the large validator.

The progression below uses seven alternating 300 ms samples per backend on the
same Apple M4 Max. Values are medians.

| State | Dragline | Railshot | D/R |
|---|---:|---:|---:|
| Six scalar operand registers | 268.17 us | 139.93 us | 1.916x |
| Cached SIMD-only memory bound | 263.59 us | 138.75 us | 1.900x |
| Cached vector constants with alias flow | 224.40 us | 139.86 us | 1.605x |
| Direct bitmask-nonzero fold | 202.87 us | 139.56 us | 1.454x |
| Cached shuffle masks | 134.62 us | 139.77 us | **0.963x** |

The final focused result is 3.7% faster than Railshot. The validator's native
body fell from 11,724 to 6,564 bytes, a 44.0% reduction. The independent
all-corpus prioritization pass measured `validateN` at 0.955x Railshot.

`json-as-simd.deserializeN(200)` is substantially improved but remains the
first unfinished corpus in the execution campaign. Direct-call structured
functions now keep their shallow scalar operand stack and integer locals in
registers across call-free regions, spill canonical call arguments and pinned
locals with paired ARM64 transfers, retain the immutable memory bound, and
outline scalar memory traps. Calls into RailMach functions consume the
callee's existing private-ABI preservation contract instead of forcing a
second caller-side local spill. Canonical JSON whitespace loops are emitted as
one compact checked loop rather than nested result-valued Wasm controls.

The progression below uses seven alternating 300 ms samples per backend on the
same Apple M4 Max. Values are medians.

| State | Dragline | Railshot | D/R |
|---|---:|---:|---:|
| Initial structured emitter | 137.54 us | 48.81 us | 2.818x |
| Pinned locals + cached bounds | 125.38 us | 49.47 us | 2.534x |
| Register operand stack across calls | 65.75 us | 49.35 us | 1.332x |
| Cold scalar traps + RailMach call contracts | 61.77 us | 49.33 us | 1.252x |
| Whitespace predicate and loop lowering | 59.68 us | 48.65 us | 1.227x |
| Paired call-boundary transfers | 58.86 us | 48.62 us | **1.211x** |
| SIMD call regions + promoted globals | 54.99 us | 49.05 us | **1.121x** |
| Adjacent-call reload deferral | 49.45 us | 48.60 us | **1.018x** |

That is a 64.0% execution-latency reduction from the initial state. Function
48 is 4,072 native bytes and the large schema parser at function 51 is 7,804
bytes, down from 4,700 and 8,344 bytes before the loop, paired-transfer, and
global-promotion work. The remaining 1.8% gap is not attributed to one parser
loop: sampling shows it distributed across the export, allocator,
vector/string, and array-parser helpers. Broadly admitting these large scalar
functions to RailMach is not safe yet: repeated execution exposed the documented
loop-edge/live-range contract gap, so the conservative SIMD-module admission
gate remains in place.

The remaining call sequence in the array parser exposed one redundant cache
operation: when one non-inlined call is immediately followed by another, the
first call's promoted-global reload is guaranteed to be clobbered before any
Wasm instruction can observe it. Deferring that reload until after the second
call brought the latest seven-round 300 ms median to 49.45 us versus Railshot's
48.60 us. The deserializer is now within 1.8% of Railshot for this campaign
slice.

`json-as-simd.serializeN(200)` is also substantially improved but remains
unfinished. Direct-call SIMD functions can now use the mixed register operand
stack: vector values are flushed to canonical slots at call boundaries while
scalar prefixes and locals use paired transfers. Structured functions with
calls promote their two hottest numeric globals into caller-clobbered
registers with write-through stores and exact reloads after every call. This
targets AssemblyScript's output and shadow-stack pointers without hiding
updates from callees.

Seven alternating 300 ms samples per backend measured 28.44 us for Dragline
and 23.05 us for Railshot, or **1.234x**. The pre-slice baseline was 54.15 us
versus 23.24 us, or 2.330x, so Dragline latency fell 47.5%. Serializer function
49 fell from 6,940 to 4,924 native bytes, and the SIMD string writer at function
52 fell from 9,772 to 6,112 bytes. The remaining 23.4% gap is concentrated in
per-item serializer/helper work and repeated explicit memory checks; broad
RailMach admission remains excluded by the same correctness gate described
above.

The same adjacent-call reload deferral removes 48 bytes from serializer
function 49, taking it from 4,924 to 4,876 native bytes. A fresh seven-round
300 ms run measured 25.58 us versus Railshot's 22.79 us, or **1.122x**. The
serializer remains unfinished, but the earlier 23.4% gap is now 12.2%.

`blake-as-simd.hashN(100)` is the next unfinished corpus. Its two large
structured compression functions declare 37 vector locals each, but ARM64 has
only 20 registers available for persistent structured locals. Use-ranked local
pinning keeps the most frequently read or written round state resident. Generic
rotate idioms select `USHR` plus `SLI`, while common `i8x16.shuffle` masks select
`REV32`, `ZIP1`, `ZIP2`, or a two-instruction lane rotate instead of two mask
materializations, table lookups, and an OR. When every shuffle has a direct
lowering and the function has no SIMD dot product, the otherwise-unused Q2/Q3
shuffle scratch registers retain two additional hot locals.

These changes were initially tested alongside an unsafe non-self call-frame
experiment, and the combined build failed an uncached digest differential. The
call change was the cause: after restricting it to self-recursion, each SIMD
change was reintroduced independently and passed a fresh differential before
measurement. Seven alternating 300 ms samples per backend now measure 460.65 us
for Dragline and 390.30 us for Railshot, or **1.180x**. The fresh pre-slice
result was 802.58 us versus 404.96 us, so Dragline latency fell 42.6%.
Compression functions 7 and 8 fell from 36,440/33,448 to 19,668/18,244 native
bytes. A diagnostic build that removed every structured SIMD bounds check did
not improve BLAKE materially. The remaining gap is vector live-state pressure;
the corpus remains unfinished.

A follow-up structured-emitter slice now recognizes `local.get` / `local.get`
/ SIMD-binary / `local.set` or pinned `local.tee` sequences even when an older
value remains on the Wasm operand stack. The emitter computes directly from the
local registers or homes into the destination local, materializing any older
aliases before overwriting a pinned destination. This removes redundant stack
traffic without changing the surrounding expression's stack semantics.

Two separately built benchmark binaries were alternated for eight 500 ms
samples each. The fused build was faster in all eight pairs: its median was
462.26 us versus 463.64 us for the immediately preceding build, a **0.3%**
latency reduction. The two compression functions fell from 19,668/18,244 to
19,452/18,028 native bytes, and the full module fell from 51,892 to 51,444
bytes. The remaining Railshot gap is still dominated by vector live-state
pressure, so `blake-as-simd` remains open for further work.

The same direct-local path now covers the verified BLAKE shuffle forms. It
shares one exact ARM64 shuffle emitter with the general stack path, including
the scratch-register rule needed when an 8-bit lane rotation would otherwise
overwrite its own source. Against the preceding fused build, eight alternating
500 ms binary pairs measured 452.97 us versus 454.23 us, a further **0.3%**
reduction; the shuffle build won seven pairs. Compression functions 7 and 8
fell again to 19,360/17,976 bytes, and the full module to 51,300 bytes.

The next pressure slice keeps one more hot vector local resident only when a
call-free structured function has exhausted every ordinary local register. It
trades the highest of eight vector operand-stack registers for that local and
threads the resulting per-function stack-register set through generic SIMD
lowering. A full SIMD differential caught and prevented an initial aliasing
mistake where generic lowering still treated the reassigned register as stack
storage. Vector local writes also receive extra pinning weight, and an
immediate `local.tee`/`local.get` pair forwards the still-live stack value
instead of loading its write-through frame home.

Against the preceding build, eight alternating 500 ms binary pairs measured a
439.49 us median versus 442.18 us, a **0.6%** reduction. Compression functions
7 and 8 fell to 19,244/17,800 bytes and the module to 50,960 bytes. A fresh
seven-round 300 ms backend comparison measured 436.51 us versus Railshot's
385.77 us, or **1.132x**. The corpus remains unfinished; the remaining gap is
still dominated by vector live-state pressure rather than bounds checks.

Read-heavy structured SIMD functions now cache the linear-memory end pointer,
not only its byte length. Bounds checks form the effective address once and
compare its access end directly, removing repeated address reconstruction.
The policy is limited to SIMD functions with at least as many loads as stores:
the all-corpus A/B rejected applying it to scalar and store-heavy functions.

Eight balanced 500 ms BLAKE pairs measured 446.87 us with the end-pointer form
versus 448.20 us before it, a **0.3%** reduction. The compression functions are
now 19,084/17,688 bytes and the module is 50,688 bytes. In a separate balanced
eight-pair JSON run, `deserializeN(200)` improved from 49.65 to 49.17 us while
`serializeN(200)` retained its existing store-heavy form; the JSON module fell
from 118,248 to 117,144 native bytes.

`mandelbrot.render(64)` exposed a missed combination between two already
verified ARM64 forms. A comparison feeding `br_if` selected direct NZCV flag
flow, but that choice prevented its single-use 12-bit constant from selecting
the immediate form. RailMach now records both combinations and emits `CMP`
immediate directly while retaining the comparison/branch fusion. The constant
producer therefore emits no native instruction.

Eight alternating 500 ms binary pairs measured a 235.24 us median with the
immediate comparison and 235.71 us before it, a **0.2%** reduction; the new
build won seven pairs. Function code fell from 572 to 568 bytes. A separate
attempt to value-number repeated floating-point multiplies through the two
inner-loop branch blocks was rejected: although the proof and alias were
valid, its longer live ranges produced no code-size win and measured slightly
slower.

The dominant gap was instead a loop-carried false dependency. ARM64's existing
edge-result rename can emit a final floating arithmetic result directly into
its destination block-argument register, but previously rejected an entire
edge when more than one result was eligible. It now considers the latest
scheduled definition; the existing checks still require one outgoing edge, one
transfer for that result, a register-to-register copy, no later use of the
destination, and no parallel-copy source clobber. Mandelbrot can therefore
write the new imaginary component directly into its loop register and omit the
copy that serialized the real/imaginary update.

Against the immediate-comparison build, eight alternating 500 ms binary pairs
measured 215.92 us versus 239.19 us, a **9.7%** reduction, with the rename build
winning every pair. A fresh eight-round alternating backend run measured
212.32 us for Dragline and 212.60 us for Railshot by median, or **0.999x**.
Function code is now 564 bytes. `mandelbrot.render(64)` is complete for this
campaign slice.

`sha256.hashN(8)` is complete for this campaign slice. Its 391-instruction
integer RailMach kernel fell below the original 512-instruction gate for
use-density allocation. Lowering that gate initially exposed a latent stage-4
regional-fragment miscompile, caught by the uncached corpus differential. The
safe policy now enables density priority from 256 instructions but limits
256-511-instruction density kernels to the verified promotion/eviction stages;
the established 512+ integer policy retains regional fragments. FPR kernels
continue to use conservative area priority.

Seven alternating 300 ms samples per backend measured 26.73 us for Dragline
and 28.07 us for Railshot, or **0.952x**: Dragline is 4.8% faster. The fresh
all-corpus baseline was 43.74 us versus 28.16 us, or 1.553x. Native code fell
from 5,900 to 5,828 bytes. The safe allocation uses six spill slots and a
112-byte frame, while reporting no regional fragments. A forced uncached
36-module differential passes with this policy.

`fib_rec.fib(28)` is complete for this campaign slice. RailMach's ordinary
function prologue already saves its incoming LR, but every private self-call
also pushed and popped LR around `BL`. That second save is unnecessary: the
callee returns to the instruction after its own `BL`, and the caller's eventual
return address remains in its prologue frame. Single private register results
were also copied into the canonical result vector even though the caller
consumed X0 directly. Proven self-recursive calls now keep the canonical
argument area but omit both operations. Other local and imported calls retain
their wrapper save and canonical staging; imported calls also retain their exact
GC stack adjustment, and multi-results retain canonical result staging.

Seven alternating 300 ms samples per backend measured 1,027.10 us for Dragline
and 1,023.25 us for Railshot, or **1.004x**. The fresh all-corpus baseline was
1,499.41 us versus 1,016.31 us, or 1.475x, so recursive execution latency fell
31.5% and is now at parity. Native code fell from 180 to 148 bytes (-17.8%).

`json-as.serializeN(200)` is improved but remains unfinished. The ARM64
finalizer now reports whether a RailMach candidate actually emitted RailMach
code before its private ABI contract is published. This closes the mixed-emitter
hole that made an earlier broad call optimization unsafe. Calls with one result
can consequently skip canonical result-vector staging only when the callee's
final emitter is proven; argument mirroring and the call-frame wrapper remain.
One-argument calls mirror the already-materialized source directly into X0,
while wider argument vectors reload pairs with `LDP`; the canonical vector is
still written first, preserving cycles and every private-entry convention.

The serializer repeatedly addresses linear memory through an unchanged mutable
global. Equivalent `global.get` addresses within one block now share explicit
bounds proofs until a global write or call invalidates them. Global-heavy
RailMach functions also retain the immutable global-descriptor array in X27,
which is excluded from allocation, saved by the private ABI, and reloaded after
every call because structured callees may use X27 internally. The abandoned X28
variant was faster but is invalid on Go/ARM64 because X28 is the runtime's
goroutine register.

The hottest mutable integer global in a call-heavy function can additionally
retain its descriptor and current value in X24/X25. `global.set` writes through
to the canonical cell before continuing, and every call reloads both registers,
so mixed RailMach/structured calls and observable global state retain the same
semantics. The selector is a bounded single instruction pass and reserves the
pair only with at least eight accesses, avoiding both quadratic compile work and
register pressure in functions without enough reuse.

Seven alternating 300 ms samples per backend measured 19.92 us for Dragline and
18.58 us for Railshot, or **1.072x**. The preceding committed seven-round result
was 21.15 us versus 18.53 us, or 1.141x, so this slice reduced Dragline latency
another 5.8%. Relative to the fresh 23.30 us corpus baseline, latency is down
14.5%. Serializer helper function 27 is now 4,540 native bytes versus 4,980 at
the baseline (-8.8%); the export wrapper is 1,060 bytes versus 1,076. A fresh
uncached 36-export differential passes. The remaining 7.2% gap is still in the
per-item serializer and allocation helpers, so this corpus remains the active
optimization target.

`many_funcs.run(5)` is complete for this campaign slice. The apparent 1.489x
execution gap was not primarily its three private calls: it matched the fixed
cost seen on other tiny exports. Railshot published this export to
`PreparedFunction` as a direct integer entry, while Dragline always entered its
serialized public adapter. ARM64 Dragline now publishes a direct prepared bit
only for finalized, register-compatible RailMach contracts without collector
root maps. A distinct bounded integer contract admits one integer parameter,
at most one integer result, no more than eight machine instructions, and only
the enumerated integer/call forms.

The three local callees have the exact validated byte-backed shape
`local.get 0; i32.const C; i32.add`. The finalizer recognizes that complete body
including its canonical signed LEB and terminal `end`, emits the add at the call
site, and otherwise retains the ordinary call. With every call proved inline,
the now-call-free caller also removes its unused LR boundary and outgoing call
area. Seven alternating 300 ms samples measured 20.02 ns for Dragline and
20.60 ns for Railshot, or **0.972x**: Dragline is 2.8% faster. The original
caller fell from 148 to 76 native bytes (-48.6%), from a 16-byte frame to zero,
and from three relocations to zero. A focused prepared-entry product test and a
fresh uncached 36-export differential pass.

A follow-up removes the residual arithmetic tree left behind by those inlined
calls. The ARM64 finalizer now symbolically verifies bounded expressions made
only from one-argument `x + immediate` callees and `i32.add`, then emits the
equivalent multiply-by-count and constant add. The matcher is capped at 32
virtual registers and uses fixed scratch storage, so unrelated functions pay no
heap cost. For `many_funcs.run`, native code falls from 76 to 64 bytes and an
eight-sample balanced before/after run moved the median from 19.14 ns to
18.84 ns (-1.6%). The complete corpus differential remains clean.

The SIMD JSON serializer's remaining structured-emitter path also materialized
every scalar memory offset into X17 before adding it, including the common
single-instruction ARM64 immediate range. Scalar bounds ends, scalar effective
addresses, and SIMD effective addresses now share the already-tested immediate
offset emitter. `json-as-simd` falls from 117,464 to 116,572 native bytes; hot
function 49 falls from 4,876 to 4,712 bytes. Ten balanced 300 ms before/after
samples moved `serializeN(200)` from a 25.59 us median to 25.32 us (-1.1%) and
`deserializeN(200)` from 48.79 us to 48.63 us (-0.3%). Scalar `json-as` is
unchanged because its hot functions already use RailMach's immediate forms.

The structured SIMD stack now also preserves the local origin established by
`local.tee` long enough to recognize the BLAKE rotate expression that consumes
that alias. Complementary constant shifts are emitted as `USHR` plus `SLI`
without separately materializing both shifted vectors and ORing them. A focused
compiler test covers the exact tee-backed expression, and the complete SIMD
differential remains clean.

Against separately built binaries, sixteen balanced 300 ms samples measured a
436.25 us baseline median and a 407.25 us fused median, a **6.6%** reduction.
A fresh paired-backend run then measured 400.0 us for Dragline and 387.3 us for
Railshot, or **1.033x**. The full module falls from 50,608 to 37,296 native
bytes (-26.3%); compression functions 7 and 8 fall from 19,036/17,664 to
12,316/11,072 bytes. `blake-as-simd` is now close but remains unfinished until
the residual 3.3% execution gap is removed.

The next SIMD slice keeps binary results in pinned destination locals across
both `local.set` and `local.tee`, while preserving any live aliases before the
write. It also recognizes an immediately preceding unpinned `local.tee` as the
source of the existing i32x4 rotate idiom; when the source and operand-stack
destination coincide, a scratch vector retains the original value for the
second half of the rotate. The full module falls again from 37,296 to 33,472
native bytes (-10.3%); compression functions 7 and 8 fall from 12,316/11,072
to 10,432/9,128 bytes.

Twelve balanced 400 ms before/after samples moved `hashN(100)` from a
400.804 us median to 382.455 us (-4.6%). A separate twelve-round alternating
Dragline/Railshot comparison measured 382.600 us versus 386.616 us, or
**0.990x Railshot**. `blake-as-simd` therefore crosses the execution gate on
this host, with a 1.0% measured lead; the complete-corpus rerank remains the
authority for the next outlier.

A second JSON SIMD address slice caches the immutable global-descriptor table
once per structured function, promotes the third hot integer global, and uses
ARM64's extended-register add to form linear-memory addresses without a
separate zero-extension. Store-heavy SIMD functions now cache the absolute
memory end as well as read-heavy functions, allowing the checked effective
address to be reused. The full corpus differential validates global mutation,
memory access, and trap behavior after each change.

Eight balanced 300 ms before/after samples moved `serializeN(200)` from a
25.091 us median to 24.189 us (-3.6%) and `deserializeN(200)` from 48.825 us to
48.113 us (-1.5%). The module falls from 116,572 to 114,660 native bytes;
serializer function 49 falls from 4,712 to 4,324 bytes and SIMD string writer
52 from 5,952 to 5,576 bytes. A fresh paired run measures 24.240 us for
Dragline and 22.557 us for Railshot, or **1.075x**; serialization remains the
largest application gap.

Pinned structured locals and promoted globals now branch directly from ARM64
comparison flags when an immediate `if` or result-free `br_if` consumes the
comparison. This avoids materializing a Wasm boolean in an operand-stack
register and copying it again for the control instruction. A focused structured
compiler fixture covers both a promoted-global `br_if` and pinned-local `if`.

Eight balanced 300 ms samples measured 24.183 us before and 23.994 us after,
a further **0.8%** reduction. Serializer function 49 falls from 4,324 to 4,244
native bytes and the module to 114,452 bytes. A fresh paired-backend median is
23.841 us versus Railshot's 22.357 us, or **1.066x**; the residual serializer
gap is 6.6%.

The next JSON slice recognizes local capacity helpers whose first operation is
an unsigned `argument <= global` early-return guard. Callers duplicate that
read-only guard and skip the complete call, safepoint, and cache-reload sequence
on the common already-capacious path; the original helper remains the exact
slow path. Across twelve balanced 400 ms samples this moves scalar
`serializeN(200)` from 19.566 us to 17.124 us (-12.5%). A paired run measures
17.075 us versus Railshot's 18.467 us, or **0.925x Railshot**.

For SIMD serializers, non-splat v128 constants now use deduplicated,
function-local ARM64 literal pools rather than repeated `mov`/`movk` materialization.
The encoder has an exact-range-tested `LDR Qt` literal relocation, and the
complete SIMD differential corpus exercises the emitted pools. The guarded
SIMD serializer first improved from 23.873 us to 23.183 us (-2.9%); literal
loads then reduced it a further 1.9% to 22.707 us in a balanced before/after
run. The final paired median is 22.784 us versus Railshot's 23.090 us, or
**0.987x Railshot**. The guarded module's 114,740 native bytes fall to 113,684
with literal pooling.

The execution worker and paired Go benchmarks now use the fixed-arity
`PreparedFunction.Invoke0` through `Invoke4` entry points whenever the manifest
signature permits it. The old worker paid variadic dispatch overhead even
though argument arity was already known; this hid the native-code advantage on
the smallest functions. With the corrected boundary, `many_funcs.run` measures
15.829 ns against Railshot's 16.476 ns across twelve alternating 500 ms rounds.

Three final ARM64 execution outliers received bounded verified rewrites:

- four-way dense local `call_indirect` dispatch prioritizes its multiply target
  and materializes the out-of-bounds trap in cold code; `dispatch.apply`
  averages 19.732 ns against Railshot's 19.800 ns over sixteen alternating
  300 ms rounds;
- small integer-only recursive functions may retain the prepared register ABI,
  so self-calls pass their single argument directly instead of round-tripping
  through the canonical stack vector; `fib_rec.fib(28)` averages 1.047 ms
  against Railshot's 1.054 ms over twelve alternating 500 ms rounds; and
- the standalone `parse4` decimal-lane expression now uses the same exact SWAR
  reduction already verified inside `runN`; it improves from 16.623 ns to
  15.794 ns versus the previous Dragline binary and measures 15.875 ns against
  Railshot's 16.090 ns over twelve alternating 400 ms rounds.

The three-round 150 ms complete 36-export rerank has a 0.893 Dragline/Railshot
execution geometric mean. Its three apparent losses were remeasured in longer
isolated alternating runs: `tiny.add` is 0.991x, `parse4` is 0.987x, and SIMD
JSON deserialization is 0.995x. SIMD BLAKE also reconfirmed at 0.997x over
twelve alternating 500 ms rounds. Every runnable manifest export therefore
crosses the paired execution gate on this host; the full compile/RSS/code-size
report still needs a post-change refresh.

## Historical application outliers

The table below predates the `blake-as` and `utf-as-simd` campaign slices and is
retained as the original diagnostic snapshot, not current ranking evidence.

The final three-round diagnostic pass produced these high-priority ratios:

| Module | Dragline | Railshot | D/R |
|---|---:|---:|---:|
| `blake-as` | 5,424.7 us | 397.5 us | 13.646x |
| `raytrace` | 2,418.1 us | 255.5 us | 9.462x |
| `blake-as-simd` | 3,452.0 us | 420.5 us | 8.210x |
| `float` | 16.936 us | 2.612 us | 6.485x |
| `nbody` | 906.1 us | 143.9 us | 6.298x |
| `spectralnorm` | 2,114.9 us | 384.9 us | 5.494x |
| `globals` | 3.109 us | 0.588 us | 5.291x |
| `matmul` | 387.6 us | 149.5 us | 2.592x |

At that historical snapshot, no application module was faster than Railshot.
The current branch has since crossed that gate for multiple modules, including
`blake-as-simd`. The next coherent work is:

1. give RailMach complete lowering for bulk memory and saturating conversions,
   then retest routing of large scalar loops;
2. carry typed floating-point operand values in FPRs instead of round-tripping
   through GPR bit representations;
3. strengthen induction/range proofs so loop memory checks can be eliminated or
   hoisted while preserving exact traps;
4. move cold trap materialization out of hot loop layout; and
5. diagnose `blake-as`, `raytrace`, and SIMD separately with counters on a
   qualified host.

## Verification

- `git diff --check`
- `go test ./...` in the main module: pass
- `go test ./...` in `bench`: pass
- 30 executable application modules, three alternating 50 ms samples per
  export, Dragline and Railshot: pass with matching execution
- focused `matmul`, five alternating 100 ms samples: pass

## Completion judgment

All numbered implementation phases in the ledger have code and verification
evidence. That is not the same as satisfying the master plan. Dragline remains
incomplete until the ledger's performance and platform gates pass, including
application execution, compile latency/allocation debt, native AMD64
qualification, and the unavailable LLVM comparison.
