# Preparation continuation: candidate INVALID

This continuation owns only `docs/rfwgc-evidence/final-measurement/`. No
production change, benchmark invocation, benchmark calibration, flock acquisition,
global-record write, subagent, push or PR occurred. These scripts and test overlays
do not enter shipped builds and cannot affect production performance.

The parent invalidated source `5345bd365c13963f1bf60e26bb80789ab9ead4cc` after
[the design review](../final-review-design/README.md). DR-1 and DR-2 are verified
regressions; DR-3 is source-only, with native Darwin/Windows execution unproven.
`candidate-status.json` is an explicit stop gate. **There is no runnable final
candidate acquisition seal.** Passing historical subsets do not override this.

`source-relation.json` preserves the exact production diff through
`942fe6c7d731ffacfac61c4aaa51bf13d5063cd3`: only the comment on
`Config.NurseryRegionShift` changed. No declaration or executable token changed.
Source positions/debug metadata may change; this does not assert identical
binaries. The pin remains 5345; the comment commit fixes none of DR-1/2/3.
Concurrent repair work in the shared checkout was neither exported nor tested.

## Completed untimed qualification

`continuation-summary.json` indexes the evidence. All raw failures remain.

| Scope | Exact completed coverage |
| --- | --- |
| Historical adapter qualification | 30 builds; 1,806 unique selector/config/mode checks; 768 canonical compile preflights; all 2,604 commands exit 0 |
| Full corpus | All 128 IDs, both revisions, all three modes, compile and runtime: 1,536 fresh rows; canonical compile preflight precedes every runtime row |
| Main logical matrix | All 316 cells / 632 sides bind to retained executables with nonempty exact censuses and matching all-three-mode qualification |
| Shape pair | Four lifecycle selectors × two immutable shape pins × three modes; separate six size checks confirm collector 200→192 and mark 192→168 bytes |
| Actual patched TinyGo | Four idle engine/size combinations; four active selectors; evacuation shift 6/12 × Off/On, Workers=1; every configuration in all three modes; 36 checks |
| Detailed native census | Six diagnostic builds and 30 test commands, all passing; graph/evacuation checks include requested workers 1/2/4/8 |
| Review-driven additions, final attempt | 12 builds and 78 test commands: six commands fail on the known defective port (DR-1 and DR-2 × three modes); three baseline object-edge cases explicitly SKIP; remaining commands pass |
| Executor unit contracts | Ten pure parser/control/gating tests pass; no acquisition or calibration is used to test the executor |

The corpus adapter uses canonical checked compilation and exact runtime/output
oracles. It never executes the prohibited baseline no-inline/lifetime-analysis
clang-cpp configuration. Unsafe historical configurations remain excluded.
The denominator still contains all 128 IDs, 121 artifact identities and all 47
input references. Captured external reference-runtime outputs remain captured
references, not fresh V8/wasmtime execution. No benchmark was called to discover
leaves: the untimed adapter emits its exact corresponding benchmark leaf names.
All ordinary/wagodebug/wago_regalloccheck leaf sets agree. No best leaf selection.

`qualification/builds.json` and `qualification/receipts.json` contain expanded
argv, complete supplied environments, cwd, modes, source binding keys, binary
hashes, log hashes and nonempty per-selector leaf receipts. `expanded-historical.json`
freezes shuffled commands for all 316 cells, with **status INVALID**, no UNBOUND
side, and 632 historically qualified sides. It grants nothing. Test-cycle leaf
censuses gate the process; the measurement unit would be one complete fresh
process, not a separate latency assigned to each subtest.

Retained executables and archived inputs are local under `.prepared/`; their
hashes, tool-tree hashes, source/overlay hashes and expanded commands are owned
committed evidence. The binaries themselves are intentionally not git objects.
`exports-final-integrity.json` checks 70,894 tracked blobs across eight completed
exports with zero undeclared drift. Only named test overlays differ. The earlier
`exports-incomplete-snapshot.json` caught the shape-after export while it was
still being extracted; it is retained as an incomplete audit, not a source defect.
Original preparation receipts remain intact and distinct from this continuation.

`additions/` retains the first adapter errors (integer width, baseline API absence,
and incompatible domain configuration). `additions-v2/` retains the corrected
intermediate run. `additions-v3/` is the final addition fixture run, including all
negative DR-1/DR-2 evidence. No failed receipt was rewritten into a pass.

## Required additions and participant census

`required-additions.json` proposes **33 additional cells**, for 349 total cells
and 8,376 accepted process samples if all qualify. This is required preparation,
**not an approved replacement acquisition budget**. The original 316-cell plan
is untouched. Parent must approve the revised bounded plan after repair/review.

* Plain non-GC externref workloads use an ordinary non-null host token, no GC
  types or conversion opcodes, 256 global/table scalar stores, or bulk
  fill/copy/init/grow of 256 elements. All six operations pass on both pins in
  all modes. These are complete compile/instantiate/invoke/token/close cycles,
  not isolated store latency. A future isolated invocation lane needs its own
  scope and budget approval.
* Independent-domain controls cover distinct producer type shapes, same versus
  separate Runtime, none/global/table externref storage, and one/two participants.
  The concurrent host-callback overlap gate passes on both pins. **Callbacks
  release invocation gates**, so this result does not waive DR-1. The preserved
  design-review lease-held probes reproduce six DR-1 failing subtests and four
  DR-2 failing subtests per mode on 5345; baseline controls pass. The watchdogs
  are correctness bounds, not performance observations.
* The old-heap fixture retains 8,192 promoted 8-KiB array payloads (64 MiB), then
  performs 8,192 ephemeral scalar allocations against 64-KiB Eden. Region 0/12,
  Workers 1/2/4/8 and object-edge source absent/present are qualified on the port.
  Baseline supports worker 1 / shift 0; its object-edge-source lane is explicitly
  unsupported, never silently treated as a pass. Setup, validation and cleanup
  belong to the full-cycle benchmark scope.

Detailed snapshots report charges, live handle/object extents by space, copied
and promoted bytes, nursery extents, mature descriptor physical occupancy and
reserved credit, free runs, evacuation demand/credit/need/movement, backing
length/capacity, and actual joined phase counts. They are endpoint observations,
not peak RSS or fragmentation. The raw `live_payload_bytes_by_space` key sums
`handleEntry.size`, including object headers; it is not user payload alone and
not cumulative bytes allocated after objects die. `allocation-byte-census.json`
separately derives the ephemeral total from 8,192 charges × the witnessed fixed
24-byte scalar extent: 196,608 bytes, excluding alignment padding. All 51
supported old-heap configurations agree. This is explicit derived fixed-layout
accounting, not a cumulative allocator counter. Diagnostic clones keep the heavy census out of
the timing benchmark bodies.

For the observed worker-8 graph fixtures, trace and sweep joined **two**
participants; this is an honestly labeled below-requested participation result,
not eight-worker scaling. Worker-8 evacuation joined eight trace/copy participants
and two sweep participants. With an object-edge source installed, the large-old
fixture asserts that multi-participant trace counters do not increase; callback
visits occur, while sweep may still join two participants. Requested worker
counts 1/2/4/8 remain intact in every receipt and proposed command.

## Machine, locks and readiness limits

`host-readonly-start.json` and `host-readonly.json` are read-only snapshots. The
host reports AMD Ryzen 7 8845HS, eight physical cores on one socket, homogeneous
family/model/stepping and frequency capability, 16 SMT logical CPUs, and allowed
CPU set 0–15. The concrete proposed serial core is CPU 0. The parallel set is
**0,2,4,6,8,10,12,14**, one thread of each physical core, identical on both sides
with GOMAXPROCS=8. No topology split or worker reduction is required. Owner must
reserve the sibling threads 1,3,5,7,9,11,13,15 from competing work too.

Visible policy is `amd-pstate-epp`, governor/EPP `performance`, AC online, and
unlimited visible cgroup CPU quota (`max 100000`). CPU temperature sources include
k10temp Tctl and legion_hwmon CPU Temperature; the ACPI temperature source is
also captured. No cooling/readiness claim follows from snapshots taken during
builds. A later grant must pin sensor names/labels as well as paths and fixed
power-policy files, and include all applicable package/core sensors.

`throttle-visibility.json` records a concrete external limitation: this container
has no qualified CPU thermal-throttling counter. Frequency variation is not a
throttling oracle. Owner must provide/qualify a host-side counter or adapter, or
explicitly revise the observability protocol. The runner rejects an empty list.

Both lock files are empty records, not grants. Their device/inode identities are
captured without acquiring a flock. Visible `/proc/locks` had no matching entry;
this PID/mount namespace observation does **not** establish global availability.
Trim and wakejoin lease records say released; wakejoin says
`samples_must_not_resume=true`. No record/inode was modified or recreated.
A released record is not ownership transfer. Canonical-then-legacy nonblocking
flocks still require global owner reconciliation and authorization.

The process view is container-scoped. Other agents/project work were active in
the snapshots, including Slipstream builds and the separate repair/review work.
No inference of host, other-container, remote-runner or Darwin readiness is made.
`preparation-drain.json` attests only this preparation's known child processes.
Global owners must attest review completion; runtime repair and fresh qualification;
local Darwin CI preparation drain; all Wago/Slipstream/other build/test/fuzz/race/
QEMU/TinyGo workers drained; no remote/namespace competing grant; blocked new job
starts; CPU sibling reservation; stable power; and both lock holders reconciled.

## Commands and executor limits

Run Python scripts from this directory. Existing evidence directories are
historical records; do not overwrite them on a repaired-source continuation.
That continuation must use a new plan pin and fresh export/evidence paths.

```sh
# Read/validate the retained historical preparation without executing workloads:
python3 validate-plan.py
python3 check-exports.py
python3 -m unittest discover -p test_executor.py -v

# Re-expand historical bindings only (output remains INVALID):
python3 freeze.py
```

The completed build/check recipes are `prepare.py`, `qualify.py`,
`qualify-additions.py`, `qualify-census.py`, and `qualify-shapes.py`; their actual
argv/env/cwd receipts are the reproducible commands. These scripts still pin the
historical source and must not be described as repaired-source qualification.

`acquire.py` is the missing future executor, supplied for review, **not executed**.
It rejects INVALID/unqualified seals before opening a lock. For a newly committed
valid seal and separate explicit owner grant, its interface is:

```sh
python3 acquire.py --seal NEW-VALID-SEAL.json --grant OWNER-GRANT.json --out FRESH-RUN
```

It checks committed seal identity and hashes; requires owner/drain/namespace/
power/sensor/throttle attestations; holds both nonblocking locks in order; uses
six alternating ABBA/BAAB blocks, per-leaf A/A and B/B checks, 250-ms telemetry,
<=60 C admission before each sample, >=85 C block invalidation, sensor/policy/
throttle/process/census error rejection, three attempts and 300-second admission
limits. It retains raw samples, rejected blocks and partial starts, never pauses
a child, and stops on scalar/noGC segment failure. Each grant allows at most a
four-hour window; the global owner must keep the cumulative two-window budget.
No automatic resume or authorization. Test-cycle timing is one process metric.

The executor's tests cover pure parsing, leaf/census rejection, paired controls,
and refusal of INVALID seals before flock. They do **not** establish live-host
telemetry integration, throttling visibility, or acquisition readiness. Future
review must validate the host adapter and grant before authorizing acquisition.
No performance direction or final-candidate readiness is claimed.
