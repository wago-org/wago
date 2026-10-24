# Direct handle space-byte loads

Base: PR948 commit `049cb7e40bb70ce3555556a674066ec4151456bb`.

The AMD64 read resolvers only need the space byte at handle offset 14.
They previously loaded the metadata word at offset 12, shifted it right by
16 bits, and masked it with 255. A zero-extending byte load has the same
result for every metadata word. It removes two instructions from each
resolution. The fixed R10/R9 sequence falls from 15 bytes to six bytes.
This is an encoding count, not a latency measurement.

The change covers the six existing read-resolver emitters in `gc_direct.go`.
It retains both space-range branches, handle and object checks, current view
loads, type checks, trap paths, and saved-register handling. It does not
change allocation or barrier metadata reads, the collector ABI, or artifacts.
The separate abstract-array cast/length emitter is outside this PR.

`LoadIdx(..., RSP, ..., 1, false, false)` emits `MOVZX r32, byte ptr [base+14]`.
Here RSP is the encoder's no-index SIB value. Writing the 32-bit register also
clears its upper half. The following TEST overwrites flags before either range
branch, so removing SHR/AND flags changes no branch input. The byte remains
inside the already-checked 16-byte handle entry. The resolver still reloads
the current handle table and backing space on each use; no address or validity
fact is retained across collection or calls.

## Validation

The Linux/AMD64 `TestGCResolversLoadSpaceByte` compiles complete modules for all six emitters
(including both defined cast and test). It requires an actual byte load in
generated code and rejects the old shift/mask. A missing `objdump` is a clear
failure. Diagnostic builds also check native call counts and shared-stub
selection. Normal builds retain the generated-code check.

`TestGCResolverSpaceByteAcrossSpaces` runs nursery, old, large and Tiny objects,
before and after collection, from fresh and reloaded native artifacts.
`TestGCResolverSpaceByteRejectsInvalidEntries` checks all invalid space bytes,
nonzero adjacent metadata bytes, and malformed handle extents and offsets.
It requires the native trap code and restores each altered entry before
teardown.

## Measurement

`BenchmarkGCResolverSpaceByte` uses complete guest-loop modules for each
changed path. Compilation and instantiation occur before the timed region.
The control and candidate test binaries share all tests and benchmark code;
a Go overlay replaces only `gc_direct.go` with its exact base version for
the control. All samples, failures and build/test wall times are retained in
`gc-space-byte-results/`.

The strict code regression failed on all seven baseline variants (4.524 s),
then passed after the six replacements (4.141 s). Corrected baseline-overlay
native safety passed in 12.215 s; the candidate register-check run passed in
16.323 s. These wall times include Go test compilation.

The first safety fixture lacked an allocating function and exact native root
admission. Wago therefore selected its legacy boundary-collection policy.
That fixture failed the explicit space assertion, and its malformed-metadata
case reached the boundary collector. The log is retained as
`baseline-safety.log`. The corrected fixture has a real allocation function
and checks exact admission and allocation collection before touching metadata.
No production code was changed to repair the fixture.

The micro lane used CPU 15, GOMAXPROCS=1, and control/candidate/candidate/control
order. Each process took two 200 ms samples per case. All 56 samples remain in
`micro-samples.json`; all report zero B/op and zero allocs/op after rounding
per guest-loop operation. These values do not prove zero allocations per
Invoke. Each run checks
its output against the guest iteration count. Task-owned builds and other
benchmarks were paused for this lane.

| Guest operation | Control median (range) ns/op | Candidate median (range) ns/op | Reduction |
| --- | ---: | ---: | ---: |
| inline-struct | 3.0030 (3.001–3.006) | 2.9205 (2.910–2.924) | 2.75% |
| shared-open-struct | 3.2720 (3.268–3.279) | 3.1700 (3.166–3.173) | 3.12% |
| defined-cast | 3.7965 (3.791–3.802) | 3.6845 (3.675–3.696) | 2.95% |
| defined-test | 3.6260 (3.622–3.633) | 3.4745 (3.467–3.486) | 4.18% |
| cast-array-length | 3.0260 (3.023–3.038) | 2.7250 (2.724–2.725) | 9.95% |
| array-reference-get | 3.8040 (3.797–3.814) | 3.6375 (3.632–3.642) | 4.38% |
| cast-struct-get | 2.9855 (2.984–2.994) | 2.8715 (2.866–2.873) | 3.82% |

These are small, hot-object loops on one shared host. They do not measure a
cache-cold handle working set or establish a full Dewdrop compiler gain.

The initial binary launch used the wrong working directory and failed before
any benchmark. Its `setup-*` records remain. The first valid control process
then exposed a parser assumption: Go omits the CPU suffix at GOMAXPROCS=1.
The parser was corrected, and that original sample was retained without a
rerun. The remaining three processes completed in the planned ABBA order.

Control/candidate binaries were built before timing, using identical tests and
this one-file overlay:

```sh
mkdir -p /tmp/wago-gc-space-byte
git show 049cb7e40bb70ce3555556a674066ec4151456bb:src/core/compiler/backend/railshot/amd64/gc_direct.go > /tmp/wago-gc-space-byte/gc_direct_base.go
python3 - <<'PYOVERLAY'
import json
from pathlib import Path
source = str(Path("src/core/compiler/backend/railshot/amd64/gc_direct.go").resolve())
Path("/tmp/wago-gc-space-byte/baseline-overlay.json").write_text(json.dumps({"Replace": {source: "/tmp/wago-gc-space-byte/gc_direct_base.go"}}))
PYOVERLAY
go test -overlay=/tmp/wago-gc-space-byte/baseline-overlay.json -c -o /tmp/wago-gc-space-byte/control.test ./src/wago
go test -c -o /tmp/wago-gc-space-byte/candidate.test ./src/wago
WAGO_SPACE_BYTE_RESULTS=/tmp/wago-gc-space-byte/new-results python3 docs/research/gc-space-byte-results/measure.py /tmp/wago-gc-space-byte/control.test /tmp/wago-gc-space-byte/candidate.test 15
```

Use a fresh results directory for a new run. The runner can resume completed
processes only when the recorded binary hashes and commands still match.

The overlay maps this worktree's absolute `gc_direct.go` path to an exact copy
from base commit `049cb7e40bb70ce3555556a674066ec4151456bb`. Source and binary
SHA256 values are recorded in `source-identity.json` and `micro-identity.json`.
The first full suites failed because this worktree lacked the pinned spec-v3
corpus and PowerShell could not find ICU (normal 214.714 s; checked 215.659 s).
The next runs restored spec-v3 and enabled invariant globalization, but lacked
the official interpreter environment (normal 30.169 s; checked 36.094 s).
All failed logs remain. The nested checked benchmark-module suite passed in
38.238 s, and passed again with the full host environment in 38.451 s.

With all pinned corpora and the interpreter, all backend/runtime packages and
`src/wago` passed in both full builds. The only remaining failure was
`TestGoInstallBuildsNamedInstallerCommand`: `GOWORK=off` prevents this older
base's installer module from using the local replacement, and its dependency
sum is absent. The normal/checked runs took 159.305/158.992 s. The exact
baseline overlay reproduced that installer failure in 8.875 s; it is unrelated
to the decoder. No installer source or dependency file changed.

The final rerun enables this checkout's existing `go.work`, with
`CGO_ENABLED=0`, `DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1`, and the pinned
Release 3 interpreter/revision. Both full suites passed: normal 15.195 s,
register-checked 15.441 s. Most packages reused the successful results from the
preceding full run; the complete root package reran and passed. The production
source hash remained unchanged throughout these environment corrections.

Required commands, after initializing the three pinned conformance submodules:

```sh
export GOMAXPROCS=2 CGO_ENABLED=0 DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1
export GOWORK="$PWD/go.work"
export WAGO_SPEC_INTERPRETER=/home/dev/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm
export WAGO_SPEC_INTERPRETER_REVISION=9d36019973201a19f9c9ebb0f10828b2fe2374aa
go test -p=2 ./...
go test -p=2 -tags=wago_regalloccheck ./...
go -C bench test -p=2 -tags=wago_regalloccheck ./...
```

The committed data also includes the initial generated-code red, the matched
baseline-overlay safety run, candidate safety/code checks, both binary build
times, every micro sample, and all broad-suite attempts. None of the build or
suite wall times is a compiler throughput claim. Log tabs and trailing spaces
are normalized for Git; every output line and sample is retained.


## Full compiler qualification on the combined stack

Three serial balanced rounds compare the control, this independent candidate,
a separate candidate and V8. Each round changes native call/decoder code only;
all Wago variants share stack d06313bc3 plus the same new refDesc physical-span
safety checks and abstract-array byte decoder. They do not include the held
collector recycling experiments. The combined refDesc implementation retains
its existing pointer/bytes fast path and adds the same span checks there.

All engines use the same compiler/request bytes, CPU affinity12,14 and
GOMAXPROCS=2. Each sample starts a fresh native cache and misses the transform
result cache. No task-owned build/test overlaps. Order is control/ABI/byte/V8,
byte/ABI/control/V8, ABI/control/byte/V8. This is a shared-host observation.

| Variant | Wall seconds | Median seconds |
| --- | --- | ---: |
| Control | 42.648727, 42.446735, 42.375415 | 42.446735 |
| This candidate | 41.395393, 41.132210, 41.185921 | 41.185921 |
| V8 | 4.058387, 3.950350, 4.008502 | 4.008502 |

This independent candidate takes 2.97% less wall
time than control. It still takes 10.27 times V8; this does
not establish parity. All12 samples have identical output Wasm/cache hashes,
validated Wasm and main42. Every Wago artifact has required and exact native
GC root admission. Full identities, commands, samples, output hashes and
admission sizes are saved in [gc-space-byte-results/full-compiler](gc-space-byte-results/full-compiler/summary.json).
No combined-gain or memory-gain claim is made from these independent samples.
