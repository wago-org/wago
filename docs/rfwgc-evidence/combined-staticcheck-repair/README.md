# Target-safe Staticcheck source repair

Qualified source and gate results are recorded in final-source.json and
results.json. The implementation commit is `6e706159a`; the evidence-only head
is reported in handoff. No later source commit is implicitly qualified.

## Eight source locations

- The two `mature_review_stage5_test.go` S1016 sites now convert free-span values
  directly to the identical `span` shape. The independent extent oracle still
  checks exactly the same offsets and sizes, without consulting handleAllocSize.
- The unused host `managedFunctionReceiptProof` placeholder and receipt slice
  are absent from the explicit host mark declaration. RV64 retains the same receipt
  slice and proof type, snapshot acquisition, identity revalidation, lock order,
  and conditional owner-edge logic.
- The host exception scratch placeholder and unused collector pointer were
  removed from the explicit host collector declaration. RV64 retains the original
  pointer and all exception scratch algorithms, transient roots, payload types,
  capture/clear behavior, and exception reclamation tests.
- The unreferenced host gate-owner helper and AMD64 conservative-mask forwarding
  helper were removed. No call site or active mask calculation changed. The
  separately built RV64 gate-owner implementation remains intact.

No reviewed baseline, lint suppression, public/container/receipt adapter,
backend, root enumeration, ownership transition, or expectation was changed.
Parent race fix `3b04e6837` is preserved byte-for-byte in
managedFunctionAfterLifetimeBoundary; see lifeMu-preservation.json. No
performance measurement was performed.

## Representation and equivalence

Before-change shape probes actually executed natively and under RV64 QEMU. RV64
collector size remains **200**, exception pointer offset **192**, mark size
**208**, receipt slice offset **160**, cursor offset **184**, done flag offset
**200**. The committed RV64 test asserts every original field's size and total
offset, plus structure sizes and alignment. All existing non-target fields keep
their representation. The full RV64 declarations are unchanged and retain the
same GC pointer shape; the proof slice element type and exception pointee are
unchanged.

On 64-bit hosts, collector size changes **200 → 192**, mark size **192 → 168**.
Only unused storage disappears; cursor/done move from 184/185 to 160/161. The
host test requires absence of the RV64-only fields and these exact total sizes.
These are Go-private types, not native object headers, FFI/persistent structures
or generated guest layouts. This still can affect Go allocation size classes and
cache behavior; no performance-neutrality claim.

Matching build constraints select one target shape for native Linux and none for
TinyGo, wago_target_tinygo or non-Linux builds, exactly like their containing
types. final-build-selection.json checks 18 combinations, including Linux 386.
This is source-selection evidence, not TinyGo or non-Linux execution
qualification.

Read-only sibling cross-checks and disposition are in sibling-readonly.json and
sibling-disposition.md. These lint findings establish no new sibling runtime
bug.

## Provenance and retained setup failures

The initial immutable pin is 563631afba41d58f6de9eb7fd31bb8aa3f16f55e. The final
source commit adds only the ten authorized source/test paths on top of
intervening docs-only commits 8dcaa7095 and f616c02ea. An unexecuted overlay
manifest is retained; the final gates run the coherent 6e706159a commit archive,
with no production overlays. The first source attempt, 6eb6d7594, passed runtime
tests but left six U1000 reports for the empty embedded host extension fields.
Its complete evidence remains under after/. Explicit target declarations remove
those fields without dummy uses. This duplicates the field lists in two target
files to preserve direct keyed construction and avoid changes to unowned
constructors; shared ownership algorithms remain single-source. Declaration
identity and exact layout assertions guard RV64. Gitlink fixtures are exported
at their exact recorded commits, never copied from dirty submodule working
files. Original archive bytes are checked after execution.

The initial Go 1.27.3 Staticcheck run hit an export-data decoder
incompatibility; its error log is retained. The actual gate uses explicit Go
1.27.1 and the project's unchanged bootstrap-staticcheck.sh, Staticcheck
2026.2.1, and unchanged reviewed-baseline driver. The project driver runs
standard, runtime, profiling, and diagnostics profiles; the reported 24 findings
were the eight locations in three finding-bearing profiles, not 24 distinct
defects.

The initial full ordinary package run failed because git archive omitted spec-v3
submodule contents. That failure and its skips remain recorded. host-replay.py
supplies pinned v1/v2/v3 archives, WABT 1.0.41, and the pinned Release 3
interpreter and reruns the complete suite. An initial fixture lookup used an
empty shared spec-v3 directory; fixture-bootstrap-failure.txt records the
corrected read-only Git object source. The initial build-tag listing template
had a quoting error; all failed calls are retained, and build-tags.py replays
using JSON and explicit selection assertions. No source expectation or safety
check was bypassed.

qualify.py and final-qualify.py each remain unchanged throughout their
respective runs. Supplemental setup drivers are retained separately; command
JSON, tool hashes, source manifests, raw logs and terminal pass/fail/skip
accounting support each verdict. Cross-build and QEMU execution results are
separate. Correctness-test durations in raw Go output are incidental, not
measurement samples.

## Performance handoff

READY TO MEASURE: branch `experiment/rfwgc-port`, source `6e706159a`. Sensitive
paths: managedFunctionCollector and managedFunctionMark allocation, joint mark
construction, owner/receipt/cycle collection and exception captures. Suggested
independent workloads: host closure-heavy owner graphs, repeated quiescent
receipt cycles, converted aggregate retention/release, and equivalent RV64 joint
mark/exception controls. Compare against parent 8dcaa7095 under the single
measurement lease. This worker has not acquired the lease, benchmarked, measured
timings, opened a PR, pushed, closed a thread or delegated work.

The private exports, test binaries and temporary tar files were removed after final integrity verification; cleanup.json records their hashes and owned path. Committed pins, exact commands, fixture pins and drivers permit reconstruction. Shared tools, dirty files, index entries and submodule worktrees were preserved.
