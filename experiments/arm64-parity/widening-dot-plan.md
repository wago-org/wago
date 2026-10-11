# Exact unsigned widening dot loop: admission plan

Research/prototype plan on Apple M4 Max arm64, starting from37174169. This is a separate general source cover, not a change to the existing i32 dot or product-fit32 pure-reduction admission. No workload identity, library limb range, manifest input or externally supplied constant is a compiler proof.

The fresh [LibTomMath profile](profile-next-20261011-libtommath-assembly.txt) identifies f28+6740..6770: four unsigned32 loads and two serial64-bit MADDs. The matching Wasm body00258c..0025ca is62 bytes. The [ranked note](libtommath-next-ranked-motifs.md) records this as candidate3. Leaf observations locate the motif; they are not cycle weights, and this kernel has not yet been established as the main runtime cost.

## Source proof and budget

Admit a void loop with empty entry stack and a bounded typed DAG scan: at most32 locals,128 nodes,24 stack entries and256 source bytes. Integer locals only. The complete body must contain exactly two `i64.load32_u` products accumulated through `i64.add` into one i64 local; two distinct i32 pointer locals advance+8 and−8; one distinct i32 counter advances−2 and compares unequal to a distinct, unwritten i32 limit; the sole branch is `br_if0` immediately followed by the loop end. Loads are memory0, width4, unsigned, and their effective source expressions are pointer+0 or pointer+4 with zero memarg displacement. Product operand order and addition association may vary, but all four load leaves must occur exactly once and match the crossed stream positions. Only these four locals may be written, with their exact final DAG values proved. No other effects, nested control, calls, stores, signed loads, trapping arithmetic, imports or bulk/SIMD operations are accepted.

Initial scope retains exclusions for memory64, shared memory, interruptible execution, EH/custom instructions, inlining-local bases, regional local pins and caller-pin-preserving leaves. An outer calling function is allowed only through its established call-free-loop entry/region mechanism; this cover does not remove existing reducers' `usesCalls` gates. An unsupported/malformed/over-budget body declines before emitting instructions. A fixed register preflight must find four ordinary GP copies and four V registers without spills, cache eviction or allocations with side effects. X16/X17 remain backend scratch.

## Dynamic trip and address proof

Let C be the initial counter, L the invariant limit and D=(C−L) modulo2^32. The vector path requires D nonzero and even; N=D/2 original iterations. D=0 is a do-while loop of2^31 iterations, not an empty loop. Odd D never reaches equality through−2; both cases enter the untouched scalar loop. A minimum vector count guard also falls back for short loops.

Every scalar iteration reads A,A+4,B,B+4, then updates A+=8,B−=8,C−=2. Before any load or source-local write, use widened arithmetic to prove all accessed bytes lie in the two ranges:

* `[A,A+8N)`
* `[B+8−8N,B+8)`

Require the lower bound nonnegative and both upper bounds≤memory bytes and≤2^32. These guards rule out every intermediate i32 address wrap and every load trap. The input pointers are read through W carriers to clear dirty upper halves. Since the body performs no stores or calls and shared memory is excluded, the streams may alias. Guard failure changes no semantic local or memory and executes the original load/trap order. Successful guards permit reordered/combined loads because all original accesses are proven safe. No extra memory outside the original ranges is read.

The final pointer updates are deliberately modulo32. B may finish at−8 after the last load window starts at0; A may finish at2^32 after the last valid byte. Restore both through W operations, preserving those wraps. Do not reject or accidentally preserve widened native pointers as guest local values.

## Vector mapping and scalar residual

Two original iterations produce four exact products. Load A lanes `[A,A+4,A+8,A+12]`. Load B at B−8 with lanes `[B−8,B−4,B,B+4]`; `REV64.4S` followed by `EXT#8` reverses them to `[B+4,B,B−4,B−8]`. `UMULL` and `UMULL2` produce full unsigned32×32→64 products. Two64-bit lane additions accumulate modulo2^64; horizontal lane addition plus the original i64 accumulator is exact because addition in that ring is associative.

Each vector batch advances A by16, B by−16 and reduces N by2. If N leaves one original iteration, restore the exact four locals and enter the original scalar header. This odd vector remainder is distinct from odd D, which never enters the vector path. If no iteration remains, restore C=L and bypass the scalar loop to its void-end join. Scalar backedges target the original header and never repeat vector initialization.

## Calling-function local-state join

The entry seam is after existing `prepareCallFreeLoopEntry`/`reconcileLocals` and `flush`, before scoped-constant preload and scalar header alignment. Empty logical stack plus completed flush ensures older deferred traps/snapshots are materialized before guards. Permanent pins are copied into protected owned temporaries; unpinned locals are read from canonical private slots. A call-making function requires `ctrlLoopCallFree` and the call-free entry/region switches so its scalar backedge preserves the same pin state.

Write each modified pinned local to its dedicated register and mark it dirty; write each modified unpinned local to its canonical slot. No call or other consumer can observe stale pinned shadow slots inside this admitted kernel. Both the vector-complete edge and scalar-residual/fallback edges converge on this representation; unrelated locals and caller pins remain intact. The join clears facts for all four modified locals, bounds certificates and store-forward state. It must not skip a needed loop-end reconciliation. Scoped constant pins are loop-private and released by the ordinary loop end.

## Qualification order

First use focused native fixtures with independent scalar oracles: arbitrary/full-width load values,64-bit sum overflow, overlapping and unaligned streams, two/three/many iterations, odd scalar residual, wrapped counters, endpoint pointer wrap, short/zero/odd-distance fallback ending in a deliberate OOB trap, first/late load traps, older deferred divide traps, outer calls and post-loop calls, high dirty i32 carriers, vector-disabled policy and compact native finalization. Verify native widening/shuffle instructions and decline all unsupported shapes, shared memory and interruptible mode. Guard-mode OOB fixtures use guarded JobMemory plus CallGuarded.

Then inspect native image changes and run only affected-image execution/compilation controls. Confirm a repeated material gain before all148 exact oracles in both bounds modes or production import. Use the shared FIFO benchmark lock for every build, test, timing and profile. No full timing sweep.
