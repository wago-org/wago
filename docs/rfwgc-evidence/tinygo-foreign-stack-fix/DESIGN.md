# TinyGo registered foreign stacks

This prerequisite belongs in the full Snap port PR (owner order November 1),
but its commits stay separate from collector changes. No files under
`src/core/runtime/gc/native` are changed by this prerequisite.

## Contract and synchronization

The pinned TinyGo 0.42.0 threads scheduler has a single `[stackBottom,stackTop)`
span per task. Wago switches SP to a separately mapped engine stack, leaving
stackTop on the original pthread stack. A GC signal while executing native code
therefore makes that span cross unmapped memory. This is pre-existing, also
reproduced on 0.43.0 in the previous investigation.

Register the entire readable engine stack once, before exposing its executable
entry thunks. The descriptor contains its low/high bounds and the location of
the trampoline's saved-host-SP cell (top minus 64 on amd64, top minus 176 on
arm64). The runtime keeps descriptors in a Go-allocated, globally rooted list;
Wago retains the returned token as unsafe.Pointer, never as a Go-heap address
hidden in uintptr or an off-heap control block.

The registry uses TinyGo's existing activeTaskLock. GC acquires that lock BEFORE
sending any pause signal and keeps it through scanning. Registration allocates
its node BEFORE acquiring the lock. Unregistration allocates nothing. Thus a
mutator holding this lock is not signalled by a collector waiting for it, and a
mutator waiting for the lock can safely be paused. Registry iteration and mapping
reads occur only while the world is stopped with this same lock held.

The scanner reads every registered mapping as a separate conservative root span.
When a paused task's bottom lies inside a registered mapping, it replaces ONLY
the bottom of that task's host-span scan with the saved host SP. It does not
replace the host top, conflate mappings, or omit either span. The current-thread
scan uses the same normalization after the existing assembly register spill.
An idle mapping's stale saved host SP is never used as a scan bound: only an
actually paused SP inside that mapping selects its cell, and every entry/resume
rewrites the cell before switching. Thus an exited thread's old host stack is
not traversed merely because an engine remains cached.
No GC suppression, retry, heap inflation, or test exclusion is introduced.

## Boundary proof

* Before a register is saved, its original host root remains in the host stack
  or interrupted register set. After saving, its foreign copy is scanned even
  while SP is still on the host stack. This matters especially during resume:
  amd64/arm64 load guest callee-saved registers BEFORE switching SP, and prepared
  entry can also overwrite a saved host register before switching. Scanning only
  the mapping containing the paused SP would be insufficient. The entire mapping
  was registered before the thunk became usable, so the union of these spans
  covers every instruction in the transition.
* Both entry forms and resume store the host SP and all ABI callee-saved
  registers before switching SP. The compiler's ordinary call ABI accounts for
  caller-saved register liveness. The foreign mapping includes the saved
  callee-saved registers, active guest frames, and the signal frame containing
  interrupted registers. TinyGo's existing signal pause handshake publishes
  these writes before the collector reads them.
* While SP is foreign, the saved host SP points at the still-live caller stack,
  including its return address; scanning that span preserves its roots without
  scanning the address-space gap. The guest stack fence retains 256 KiB of
  low-end headroom for the signal/runtime frames.
* Host calls restore the host SP before invoking Go. The mapping remains
  registered while guest frames are parked. Reentry uses a separate leased
  engine; every mapping remains registered. Resume overwrites its saved-host-SP
  cell before switching back, so it belongs to the actual resuming host task.
* Normal return, handler-jump traps, and cancellation all restore the same save
  area. During a partial epilogue, saved registers remain scanned until SP
  becomes host; afterwards the host registers/stack are scanned normally.
  Trampoline creation failures never publish an unregistered live stack.
* Close requires the existing engine lease rule: no running native call, parked
  continuation, or borrowed stack address. It unregisters under the GC lock before attempting either unmap. A concurrent GC
  either finishes scanning the mapped stack before removal, or never sees it.
  Even if thunk unmapping fails, Engine.Close still attempts stack unmapping;
  removing registration first avoids a dangling GC span. A subsequent Close
  retries thunk cleanup without unregistering twice.

Serial `tasks`/`none` schedulers cannot collect during native execution: native
code does not allocate Go objects or call Go on the foreign stack, and host
callbacks first restore the host context. They need no registry and keep that
path. Unsupported scheduler configurations do not silently select the serial
implementation.

## Costs and limitations

This deliberately conservative implementation scans the full registered mapping,
including idle/parked stacks. Work is bounded by registered engine capacity and
lifetime. Stale pointer-shaped words may retain Go objects (and their reachable
graphs) until overwritten or the engine closes, as with conservative roots.
The registry does not transfer ownership of mappings or extend an engine lease.
Guest control frames still carry numeric/reference handles and off-heap
addresses under Wago's existing ownership rules; this change stores no Go heap
owners there and does not authorize hiding a Go pointer in an unscanned control
frame. Existing host-side anchors remain required independently of stack scans.
No performance claim is made: allocation/Close locking and GC scanning are
performance-sensitive. Independent measurement is required before deciding
whether a smaller active-span protocol is warranted.

The deliverable is a reproducible repository patch/bootstrap, not an untracked
edit to upstream TinyGo. Bootstrap checks exactly 0.42.0, validates original
source SHA-256 values, accepts exact already-patched outputs, and rejects drift
or partial patching before writing its foreign-stack edits. Unpatched threaded
builds fail to link the required hooks. This does not claim compatibility with
unpatched 0.43.0 or execution qualification of arm64 on this amd64 machine.

Read-only sibling audit extends the original investigation: Slipstream still
marks this TinyGo artifact surface not applicable; Starshine's inspected src/tests
have no matching TinyGo foreign-stack integration. No transferable fix or new
sibling production defect was found, and neither sibling was modified.
