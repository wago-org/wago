I found one concrete change on the allocation side. It removes repeated checks of reference fields when code builds new objects. I read code only. I did not build, test or measure anything, so I cannot promise a speed gain.

## Proposal: skip the field check when the value is a fresh object

**Where (source fact):** `src/core/compiler/backend/railshot/amd64/gc_direct.go:1803-1895`, in `emitNativeStructAlloc`.

For each `struct.new`, the code loops over every field that holds a reference. For each one it checks the value again at run time:
- Is it null or i31 (a small integer packed into the reference)?
- Is the handle in range (`NativeViewHandleCountOffset`)?
- Is the space byte live (not Free, at most Tiny)?
- For fields with an exact type: does the object fit inside its space, and is its type correct (an exact compare, or a subtype-interval compare)?

That is about 8–10 loads, where each load waits for the one before it, plus up to 9 branches per field. The comment at line 1839 gives the reason: "host-supplied or stale reference".

**Repeated work (inference):** Code often builds a tree of objects, like `struct.new A (struct.new B …)`. The inner value comes straight from a guest allocation that just finished. Its handle, live space, size and exact type are already known when the code is compiled. The check proves those facts again each time.

**Change:**
1. **Mark fresh values.** Where an allocation result goes onto the operand stack (`callNativeStructDirectSlots` at `call.go:1381`, the inline path at `call.go:1458`, and the array allocation result), mark that stack entry as "fresh object of canonical type T". V1 tracks it only while the value stays on the operand stack. Remove the mark when the value goes into a local or is copied, and at any merge point in the control flow.
2. **Use a second entry when the facts hold.** In `emitNativeStructAlloc`, if every reference-field operand is either fresh with a type that is a subtype of the field type, or a constant `ref.null`, emit a variant with no per-field checks.
3. **Limit code growth.** Today shared stubs are keyed by `typeIndex` (`gc_direct.go:1077-1092`). Change the key to `(typeIndex, trusted bool)`. This allows at most 2 stubs per type. It is not one stub per call site, so it does not repeat the 4.7%-slower experiment. The inline path decides at each site and adds no stub.
4. **Assert.** If a fresh operand's static type is not a subtype of the field type, stop the compiler with a panic that names the type and the field. Keep a build flag that forces the checked path, so you can compare the two outputs.

**Proof rules:**
- The operand is a GC root in its canonical slot, so the collector cannot reclaim its handle. A collection between the allocation and the use can move the object, but the handle is stable.
- Promotion changes the space byte to Old, which is still valid. The object's type does not change.
- The allocation and the use are in the same function and the same instance, so both use the same collector view (`R8`).
- Values from the host, globals, tables, parameters, `call` results, `struct.get` or locals stay on the full check in V1.
- The fallback path must marshal the same slots in the same way as today.
- The order of failures must not change. Each skipped check can only pass for a fresh object, so the skip cannot hide a trap.

**What it does not repeat:** getter inlining, test-field prefixes, stable-handle recycling, stubs for each call site, numeric inlining, direct forwarders, the reference-register calling convention, and the cached parent wrapper. `gcFreshStructFact` (`gc_struct.go:290`) is a different fact: it helps reads of one-field wrapper objects, not constructor operands.

## Smallest measurement that decides it

1. **Count at compile time (no timing).** For the pinned compiler Wasm, count:
   - `struct.new` sites
   - reference fields
   - fields whose operand would be marked fresh
   
   If almost no fields qualify, stop.
2. **Count at run time (separate build, not timed).** Add a plain counter for each instance at entry to the checked block, split into "would qualify" and "would not qualify". Write the counts once when the job ends.
   - Rough threshold (inference): if fewer than about 10⁸ checks would be skipped, the gain cannot be clear against a 36 s job.
3. **Run the full A/B test only after that.** Use the same Wasm and request, compare output hashes and diagnostics, and record user CPU, code bytes and RSS (memory use). A small kernel that builds nested lists of two and three fields shows the best-case gain on its own.

## Bounded implementation

- About 3 files: `gc_direct.go`, `call.go`, and the file that sets the operand-stack entry for allocation results.
- One new 1-bit flag on the stack entry, a stub key that is a pair, and one assertion.
- Tests:
  - a nested constructor
  - a fresh value sent through a local (must still check in V1)
  - a value from the host (must still check)
  - a collection between the inner and outer `struct.new` (move or promote, then the outer fallback runs)
  - an exact type vs. a subtype field

## Facts, inferences, measurements

- **Source facts:** the check block, the stub keyed by type, and the reason given in the comment.
- **Inferences:** that constructor trees are common in the compiler Wasm, and the cost estimate for each field.
- **Measurements:** none.

**What to do now:** run the step 1 compile-time count first. It is cheap, and it tells you if this is worth doing.
