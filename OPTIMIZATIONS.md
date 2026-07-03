# wago optimization roadmap

Two complementary lenses on the same question — *how do we make wago faster without
destroying the reason it exists* (fast compile, no cgo, tiny footprint, single pass):

1. **Make the single-pass backend smarter** — better-informed choices inside the existing
   railshot tier.
2. **Port what's still worth porting from WARP** (`warp/`) — the C++ reference engine the
   backend is a port of. Used as a *reference axis*, not a target to clone.

The headline architectural decision (see the end): **keep two tiers and do not blend them.**
The single-pass tier stays single-pass; the `src/core/compiler/ir` SSA package becomes an
*optional* optimizing tier later, never something a plain `Compile` pays for.

Legend: effort S/M/L · value ⬜ low · 🟦 medium · 🟩 high · ⭐ very high.

---

## What's in place (updated 2026-07-02)

The backend (`src/core/compiler/backend/railshot`) is the full WARP-architecture port: a
single-pass x86-64 codegen over a valent-block operand stack (deferred-action trees,
condense engine) with an on-the-fly whole-register-file allocator. Landed, in rough order:

**Storage model / register allocation**
- **Register-ABI internal calls** (old P1) — args/results in registers between wasm
  functions; wrapper ABI kept at the Go boundary. Includes the parallel-move resolver.
- **Hotness-aware local pinning** (old P2) — loop-weighted scores from a one-pass
  `scanBody` pre-scan (`hints.go`), WARP-style whole-file pin pool for call-making
  functions too (up to `file − 4 scratch`), STACK_REG lazy spill (dirty-only stores at
  calls, lazy reload) for **all** call-making functions. #68's real root cause (the
  `opElse` merge edge skipping reconciliation) was found and fixed with regression tests.
- **Value-pinned hot globals** sharing the pin pool (#84–#86).
- **memBytes in R15** (old P3) — explicit-bounds mode keeps the memory size in a
  module-wide reserved register (WARP `REGS::memSize`); checks are `lea; cmp; ja stub`.
- **Lazy per-frame merge agreement** (old P6, locals half) — control-flow edges agree
  per-frame on each pinned local's merge state (`lsStackReg` or `lsMem`), so a
  call-clobbered local can stay slot-only across a merge until actually read. Loop tops
  stay eager (reloads hoisted out of bodies). Conditional returns converge nothing.

**Bounds checks / traps**
- **Guard-page mode** (old P5) is first-class behind `-tags wago_guardpage` and is the
  *default* bounds mode in such builds (`WAGO_BOUNDS=explicit` overrides).
- **Shared cold trap stubs** (old P9) — one stub per trap code per function; every check
  is a fall-through `ja stub`. (~23% smaller code on memory-heavy modules.)
- **Stack-fence elision for small call-free leaves** — a leaf's one unchecked frame is
  absorbed by the fence's 256 KiB margin.

**Instruction selection**
- Compare→branch fusion; constant folding; memarg offset folding; deferred loads folded
  as ALU r/m operands; in-place accumulation; cmov select.
- **Algebraic identities + strength reduction** (old P4) — `x±0`, `x&~0`, `x|0`, `x^0`,
  shifts by 0, `x*1`, `x*0`, `x*2ⁿ→shl`, `x*{3,5,9}→lea`, `x/ᵤ2ⁿ→shr`, `x%ᵤ2ⁿ→and`,
  `x-x`/`x^x→0`, `x&x`/`x|x→x` — at `pushBinOp`, before a node exists.
- **Scaled-index LEA fusion** — `add(x, shl(y, k≤3))` → `lea [x + y*2ᵏ]` (the
  AssemblyScript array-address shape).
- **`br_table` jump tables** (old P7) — n≥5 dispatches through a RIP-relative offset
  table with deduplicated per-case stubs; smaller tables keep the cmp/jne chain.
- **Small constant `memory.fill`/`copy` unrolled** — n≤32 lowers to overlap-safe
  load-all/store-all chunks (memmove semantics preserved); no `rep` microcode startup.
- **`call; local.set` result fusion** — a register-ABI call result lands directly in the
  pinned local's register.
- **Register-ABI `call_indirect`** — the table entry's pad word carries the internal-entry
  delta, so compatible signatures skip the wrapper adapter.
- **Code layout** — 16-byte aligned functions, internal entries, and loop tops (multi-byte
  NOPs on the entry path). Tight-loop benchmarks swing ±20% on layout luck without this;
  treat any single-module regression as suspect until the disassembly is diffed.

**MVP completeness** (old "completion batch"): memory.grow/size, trapping float→int
truncation + trunc_sat, start function, multi-value, imported/mutable globals.

**Compile speed**: decoded modules keep byte-backed function bodies. The optional
`scanBody` instruction walk is used only for programmatically constructed modules that
provide decoded instructions; normal decoded modules use BodyBytes and first-N pinning.

### Measured (2026-07-02, explicit bounds, vs the pre-sweep #87 baseline)

| bench | #87 | sweep | Δ |
|---|---:|---:|---:|
| sieve | 163µs | 123µs | **−24%** (beats wazero) |
| memory_tree | 14.6µs | 11.8µs | **−19%** |
| linked_list | 11.3µs | 9.4µs | **−17%** |
| dispatch (call_indirect) | 19.1ns | 17.6ns | −8% |
| blake-as | 729µs | 700µs | −4% |
| json-as ser / deser | 218 / 396 | 197 / 204 | −10% / **−48%** |
| memory.sum (explicit vs guard) | 337 | 230 | **explicit == guard** |

Cumulative from before #87 (main@22c09be): json ser 257→197, deser 420→204;
memory.sum 552→230; sieve 165→95; memory_tree 17.2→11.6; wazero-relative json
0.56x→0.72x ser / **0.70x→1.43x deser (wago now wins)**. wago beats wazero on
fib_rec, sieve, memory_tree, linked_list, dispatch, branches, and json deserialize;
loses on json serialize and blake.

The deserialize flip came from running WARP itself on json-as (passive/bounds-off
build, ser 97ns / deser 164ns per unit) and replicating its remaining structural
edges: no per-call environment protocol (RBX/linMem as module invariant, trap cell
in basedata — no trap-clear on returns), module-wide global register pinning (the
AS shadow-stack pointer), pinned-register-borrowed load addresses, and — decisive
for deserialize — an inline 8-byte chunk-loop memmove for small dynamic
memory.copy/fill instead of `rep movsb` (whose startup latency dominated the
string-append copies AssemblyScript's `__renew` makes constantly). wago-guard
deser is now within 1.13× of WARP.

---

## Remaining roadmap (priority-ordered)

### R1. `vFlags` — compare fusion past adjacency  · M · 🟦 (old P8)
Fusion only fires when the branch immediately follows the compare. Misses
`cmp; local.tee $c; br_if` and `eqz; local.set/get; if`. Add a flags-resident stack
entry carrying its `Cond`, materialized the instant any flag-clobbering op appears.
Start pattern-constrained; don't birth a second compiler.

### R2. Float lowering parity  · M · 🟦
ISA suite lags: floats ~1.65× (no in-place XMM accumulation — the int path has it),
min/max 2.2× (branchy lowering; use `minss/maxss` + NaN fixup), and float pinned locals
use the eager call model. Mechanical, well-scoped.

### R3. Store-narrowing peephole  · S · ⬜
`setcc; movzx; store8` keeps a dead `movzx` (sieve's inner loop). A deferred-compare
consumer that stores 8 bits can skip the widening. Cheap, narrow.

### R4. json serialize gap (deserialize is solved)  · M–L · 🟩
Deserialize now beats wazero and sits 1.13× from WARP. Serialize remains ~2× from
WARP: 52% of it is one function (the serializer core, local fn 26 / wat 27)
writing JSON text through global bump pointers in `global.get; i64.store;
global.set` bursts punctuated by ensure-capacity calls. **Correction to earlier
text in this section:** `pickModuleGlobals`'s aggregate score for the json-as
bench module is **global 2 (score 4603) — the serializer's own output
write-pointer** — not "the AS shadow-stack pointer" as PR #90's description
assumed; that guess was never verified against this module's actual global
indices. Global 4 (score 1350, the capacity watermark) is the *second*-highest
candidate; a third candidate (global 25, score 737, identity unconfirmed — see
below) is a distant third. Verified with a temporary
`WAGO_DEBUG_MODGLOBALS=1` print in `pickModuleGlobals` (not shipped).

**B1 landed (borrowed reads for value-pinned globals, `stGlobReg`):** `global.get`
on a value-pinned global used to copy the register out (`materialize`'s ~30
consumer sites all forced a copy); it now pushes a borrowed reference like a
pinned local (`stLocalReg`), realized on `global.set`/flush/call-arg staging.

**B2 landed (immediate-only constant stores):** `i64.const; i64.store` used to
materialize a 10-byte movabs into a register before storing; `memStore` now peeks
the value for `stConst` and emits `mov r/m, imm` directly (two 4-byte immediate
stores for i64, matching width for i32/i16/i8), via a new encoder op
`StoreImmIdx`.

**B3 (WARP wat-27 diff):** Regenerated `/tmp/warp-json.bin` and disassembled both
sides for the identical burst (`{"authenticated":` — the same struct field name
appears in both benches' schema, confirmed by decoding the movabs immediates as
UTF-16LE). The comparison **reverses the original hypothesis**:

- **WARP does not keep the write-pointer in a register across the burst at all.**
  It reloads it from a *fixed basedata offset* (`mov r10d,[rbx-0xf0]`) before
  **every single 8-byte store**, and writes it back once at the end — no
  cell-pointer indirection (globals live directly in basedata, one load, no
  array-of-pointers), but no register residency either. Per store: reload (7B) +
  movabs (10B) + store (4–5B) ≈ 3 instructions, ~21 bytes.
- **wago (current, K=1, post B1+B2) keeps the write-pointer live in R14 for the
  *entire* burst** — zero reloads between stores, bumped once at the end
  (`add r14d,0x22`) — and B2 means zero movabs. Per store: one 7-byte immediate
  store, period. For this exact burst wago now emits **fewer instructions and
  less code than WARP**, with fewer memory accesses.
- Confirmed by re-running the K-sweep with `WAGO_DEBUG_MODGLOBALS`: **K=2
  {R14,R13} already pins BOTH burst globals** (2 and 4 are the top two
  candidates by score) — this corrects the earlier claim that K=2 "only fits one
  of {2,4}". Redisassembling fn26 at K=2 confirms global 4's capacity-watermark
  bump goes fully register-resident too (`add r13d,0xaa`, no memory access) —
  the codegen change is real. **But json-as ser/deser measured flat at K=2
  anyway** (~193–200ns, indistinguishable from K=1, noisy). K=3's ~6–8%
  improvement therefore is NOT explained by "both burst globals finally pinned
  together" (they already are, at K=2) — it must come from module-pinning the
  *third* candidate (global 25, score 737), whose identity and mechanism of
  effect are **not yet confirmed** (possibly it helps a different hot function
  entirely, since module-pinning benefits every function touching that global,
  not just fn26 — fn26's own burst codegen doesn't visibly change between K=2
  and K=3 in the region inspected).
- **fn26 is still 52% of the serialize profile** (`perf record`/`report`,
  unchanged from the original figure) despite the burst codegen now beating
  WARP's instruction-for-instruction. This means **register residency of the
  burst globals is not the remaining bottleneck** — B1+B2+the K-sweep have
  exhausted what this angle can give. fn26 makes ~9–10 calls per invocation
  (ensure-capacity checks + nested field writers); call/return overhead and
  per-call setup are the next suspect, not yet measured or diffed against
  WARP's equivalent call sites (attempted but the WARP blob's `call`-target
  region didn't disassemble cleanly as a standalone slice — linear disassembly
  desynced past a jump table or embedded constant; needs re-attempting with
  proper function-boundary recovery, not a raw linear objdump).

**Net:** ser/deser measured flat at the shipped K=1 default (both B1 and B2 are
real, verified codegen improvements that don't move wall-clock time on this
Zen4 machine — plausibly because register-vs-memory arithmetic for a few
extra loads per burst is already hidden by out-of-order execution and
store-to-load forwarding). K=3 gives a real ~6–8% win via a not-yet-understood
third global, at a real ~7% cost to blake (already behind wazero there) —
shipped K=1 (see the K-sweep judgment call, still valid). Next session should
profile the *call* overhead in fn26, not further chase global register
residency.

### R5. Runtime / infra from WARP
| Item | Effort | Value | Notes |
|---|:--:|:--:|---|
| Interruption / cooperative cancel | S–M | 🟩 | `checkForInterruptionRequest`; kills runaway loops |
| Wasm-level stack trace on trap | M | 🟩 | frame-ref chain |
| Debug mode + bytecode→machine map | M | 🟦 | |
| arm64 backend | L | 🟩¹ | WARP `backend/aarch64` as reference |
| Sync host calls w/ return values (V2 imports) | L | ⭐ | runtime half spiked; biggest functional unlock (WASI) |

¹ if Apple Silicon / arm64 Linux matters.

### R6. Measurement hardening  · S · 🟦 (old P0, still worth doing)
`CodegenStats` (spills/flushes/bounds-checks/code-bytes per function) + golden
disassembly tests per optimization. The sweep found layout-luck swings of ±20% on tight
loops — per-function code-byte tracking would catch silent bloat, and golden disasm
would catch silently-disabled peepholes.

### Greenfield (not in WARP either)
SIMD/v128, threads & atomics, exception handling, tail calls, full reference types +
`table.*`, remaining bulk-memory (`memory.init`/`data.drop`), passive segments,
memory64, multi-memory, imported memory/table (the `linking`/`data` spec files).

---

## The one architecture choice

Keep **two tiers, unblended**:

- **Tier 1 — single-pass baseline JIT** (railshot): `validated wasm → scanBody pre-scan →
  single-pass codegen`. Goals: very fast compile, good-enough code, tiny footprint,
  predictable. Everything above lives here. This tier is now broadly at or beyond
  WARP-parity per construct; its remaining structural ceiling is register allocation
  across basic blocks, which is exactly what Tier 2 is for.
- **Tier 2 — optional SSA optimizing tier**: the `src/core/compiler/ir` scalar SSA
  package exists but no runtime path imports it. Use it only for hot modules / AOT
  `.wago` / an explicit `Optimize` option — never on the default `Compile` path. The
  grounded case for it: wazero's remaining edge on json-as is its SSA register
  allocator, not its (minimal) optimization passes.

wago's identity is **low-latency compile**. The single-pass tier is now informed,
flush-light, and register-resident; SSA is the expensive opt-in tier, later.
