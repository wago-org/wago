# PR 980: explicit scheduling points for ARM64 live host loops

The failed-job retry still fails the Darwin ARM64 platform job:
https://github.com/wago-org/wago/actions/runs/39606585738/job/119665056637

This time the typed and Caller forms of
`TestBoundedHostYieldAllowsGCAndCancellationWithOneP` reach the existing
three-second GC progress limit. The prior attempt failed the HostCall form.
The final CI job reports the platform-test failure; it has no distinct compiler
error. The Darwin AMD64 corpus shard now passes without a source change.

## Cause and change

The live ARM64 bridge owns a NOSPLIT Go assembly frame and jumps through native
code between callbacks. Bounded guest work between callbacks does not guarantee
an explicit Go scheduling point: very small callbacks may have no stack check.
Repeated callbacks can keep the single P busy and delay a requested collection.
The original ordinary fixture still passes under Linux/QEMU; that does not
invalidate the repeated native Darwin failure.

The initial direct-runtime fixture used 65,536 callbacks with no stack-growth checks
and asks a second goroutine to collect. It requires the final callback to see
that collection completed before the guest returns. With asynchronous
preemption disabled, all eight live ARM64 owner variants fail on unchanged
production code. This is a bounded, reproducible failure independent of a
three-second wall clock limit. Their cancellation variants require an interrupt
raised during the scheduling point to return to Go without native resumption.

Each live ARM64 owner now keeps a private countdown. After every 1,024
callbacks it calls Go's `runtime.Gosched`, while native state is parked and the
owner's Go argument map is active. Callback results are stored before the call;
control and trap pointers are reloaded afterward. A new cancellation check
prevents native resumption when another goroutine interrupts during the yield.

The scalar/countdown slot is 48(SP), outside its 24-byte outgoing Go argument
area and parked words at 32/40(SP). View owners use 80(SP), outside outgoing
slice/context arguments and parked words at 64/72(SP). Existing 64/96-byte
frames do not grow. Owner entry always initializes the counter, so short calls
and nested owners do not share it. Context owners use their distinct trap and
control argument offsets (40/64 rather than 32/56). No new heap state, public
ABI, native root metadata, artifact field, or native admission is introduced.
The common callback path adds a load, subtract, store, and branch. There is no
Go call on that path until the countdown expires. AMD64 code is unchanged.

## Validation and limits

The new test fails for all eight owner types before the repair. The fixed test,
including interruption at the yield, passes with register checks and async
preemption disabled under Linux ARM64 QEMU. Existing recursive Go stack-growth,
FP preservation, bounded helper, and live admission tests also pass there.
Ten repeats of the public bounded host tests and general array.len guard,
collection, codec, invalid-reference, and live-vector tests pass with statistics
and register checks. Darwin ARM64 runtime and Wago test binaries build, and the
Mach-O context-owner disassembly shows the correct slow call, reloaded trap and
control offsets, and cancellation branch. QEMU is correctness evidence, not a
native Darwin or performance result. No online Darwin device is connected;
the next hosted platform job must confirm the native behavior.

A broad QEMU runtime run also reports signal-ownership `invalid argument` and
repeated job-memory pool-cache expectation failures. Unchanged PR head reports
the same two failing test names under the same emulator and flags. Keep both
raw outputs; do not label that broad emulated suite green or weaken its checks.
The focused runtime and host/array checks above are green.

Early invalid attempts are retained: the initial raw fixture omitted its trap
pointer binding, and the first macro used non-context argument offsets for
context owners. The fixture and per-owner offsets were corrected before the
final passing checks. The valid pre-fix regression log is `leaf-callback-arm64-red-r2`.

Go 1.27.1, GOMAXPROCS=2; package concurrency 2 for broad native gates and 1
for cross-target work. Required local gates pass. Times include builds.

| Check | Wall seconds | Peak RSS KiB |
| --- | ---: | ---: |
| Complete ordinary root suite | 151.407 | 3,223,636 |
| Complete register-check root suite | 135.766 | 3,228,448 |
| Register-check benchmark module | 30.188 | 896,660 |
| ARM64 runtime focused checks (10 repeats) | 1.404 | 101,184 |
| ARM64 public host/array stats checks (10 repeats) | 26.170 | 2,427,668 |
| Darwin ARM64 runtime test build | 1.231 | 137,740 |
| Darwin ARM64 Wago stats/check test build | 25.040 | 2,357,004 |

The documentation gate passes for 84 Markdown files. Long test/build activities
exceed the 30-second target and remain recorded. No native ARM64 timing or
unchanged-host speed claim is made for this correctness fix. The pinned spec
corpus was supplied by a temporary shared local Git clone and removed after the
root suites. No timeout is increased and no test is removed. The original
three-second public GC/cancellation limit stays intact. Hosted Darwin validation
is still required after the push; another retry of old failing code is not used.


## Linux ARM64 test synchronization follow-up

The [Linux ARM64 job](https://github.com/wago-org/wago/actions/runs/39737641460/job/119687183896)
on `ed37834` fails only the new leaf-callback scalar case: the final callback
still sees zero after the fixed 65,536-call loop. That loop assumes collection
must complete within 64 yields. Scheduler access does not imply completion in
that short window, especially during a process's first collection. The public
three-second host-GC test passes in this job.

The guest test now exits when its callback observes collection completion.
A hard cap of 16,777,216 callbacks still makes a non-yielding bridge return and
report a diagnostic failure. This cap is a safety stop, not the normal exit.
An atomic callback count gates the collector: collection starts only after
native execution begins, so a collection before entry cannot satisfy the test.
All callbacks remain NOSPLIT and contain no explicit scheduler call. Cleanup
joins the collector, including a failure before the first callback. The eight
owner paths and all interrupt outcome/trap assertions remain in place.

A negative control temporarily disables the production scheduling macro only
while building a separate test binary, then restores the header. With async
preemption disabled and GOMAXPROCS=1 in the test, the scalar and scalar/cancel
cases both reach the hard cap and fail with explicit progress/outcome messages:
3.559 seconds, peak RSS 29,876 KiB. This control confirms that waiting for the
event does not mask the original scheduler-starvation fault. The negative
binary build takes 0.838 seconds, peak RSS 142,484 KiB.

The unchanged production bridge passes 30 repeats of the leaf, stack-growth,
FP preservation, admission, and bounded-helper checks under ARM64 QEMU with
register checks and async preemption disabled: 2.376 seconds including build,
1.630 seconds for the test package, peak RSS 137,872 KiB. The default-build leaf test also passes 30 repeats: 1.832 seconds including
build, peak RSS 141,968 KiB. The Darwin ARM64 runtime test binary builds in
0.771 seconds with peak RSS 142,032 KiB. The documentation check passes in
0.136 seconds with peak RSS 69,544 KiB.
This update changes the test and this note. The runtime's 1,024-callback yield
interval and the public three-second GC/cancel limit remain unchanged. Native
Linux and Darwin CI must still confirm their platform suites.
