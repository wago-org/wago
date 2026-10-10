# Production isolation qualification

The user explicitly prohibited merging #911 and requested fresh production
isolation proof. The merge worker's clean integration `ed92d0f18` was copied by
fast-forward into the owner's separate checkout. Its main parent is
`209e448c392510a0325d5b282a0d86a776fb379c`; the worker's checkout was not edited.
That integration preserved both changelog entries when incorporating #875.

## Opt-in correction

Production targets did not import the diagnostic, but its untagged command was
included in broad `go build ./...` and install discovery. Every diagnostic Go
file now requires `wago_nativecompare`, including tests. The recipes explicitly
supply it; capture additionally requires AMD64 and `wago_profile`. Ordinary,
runtime, minimal, profile-only and codegenstats-only discovery excludes it.
An existing Linux AMD64 CI cell runs the opted-in recipe, including an exclusion
regression that would catch a newly added ungated helper or test file.

The tag correction changes no comparator, source-map or capture algorithms.
There are no effective changes to production Go files, module dependencies,
generated facade/schema, release scripts or production build recipes against
the matched main control. No production target imports `tests/tools/native-compare`.
Existing profile/codegenstats instrumentation remains the same as main when
those pre-existing tags are explicitly enabled.

## Matched Go evidence

Go 1.27.1, one build worker, matched settings, `-trimpath`, `-buildvcs=false`, an
empty linker build ID and a fixed version stamp remove irrelevant checkout/VCS
differences. Executables are unstripped so their symbols can also be inspected.
These are controlled build comparisons, not benchmark timings or release-asset
signatures. All paired executable SHA256 values and sizes are retained.

| Target | Manager | Standard runtime | Just minimal | Release minimal | Embedded runtime |
|---|---|---|---|---|---|
| Linux AMD64, CGO disabled | identical | identical | identical | identical | identical |
| Linux ARM64, CGO disabled | identical | identical | identical | identical | identical |
| Darwin AMD64, CGO disabled | identical | identical | identical | identical | identical |
| Darwin ARM64, CGO disabled | identical | identical | identical | identical | identical |
| Windows AMD64, CGO disabled | identical | identical | identical | identical | identical |
| Windows ARM64, CGO disabled | identical | identical | identical | identical | identical |
| Linux AMD64, CGO enabled | identical | identical | identical | identical | identical |

All 35 paired executables are byte-identical to main. The original symbol scan
checked the package import path; independent review correctly noted that command
functions use `main.*` names. The corrected script checks distinctive command
symbols too, and a separate hash-matched replay records the corrected scans in
[symbol-audit.json](symbol-audit.json). Just minimal uses `wago_runtime,wago_lean,wago_minimal`; the Go
release script uses `wago_runtime,wago_minimal`. The embedded target is the existing
public-runtime DCE probe. The separate installer module is source-unchanged and
depends on a released module version; its executable was not compared here.

Production input manifests hash selected Go/assembly/C/embedded files and import
lists for `./cli/wago` and the public API. They match under ordinary, runtime,
both minimal tag sets, profile, codegenstats, runtime/profile, runtime/codegenstats
and runtime/tool-tag modes across the six platforms, plus native CGO-enabled
controls. Ordinary broad package discovery excludes the diagnostic in every
tested platform's ordinary/runtime/profile/codegenstats modes. Explicit tool-tag
discovery includes it and the focused tests exercise its functionality.

Raw evidence: [primary Go controls](go-primary.json),
[release-minimal controls](go-release-minimal.json),
[native CGO controls](go-native-cgo.json) and
[native execution](native-execution.json). The standard and Just-minimal Linux
AMD64 runtimes both produce `fib(20)=6765` from the trusted repository fixture.
The initial minimal smoke command rejected standard-only `--bare`; the corrected
command used supported minimal flags and passed. No compiler failure was inferred
from that CLI-flag mistake.

For these matched Go targets, unchanged selected production inputs and identical
linked executables rule out newly compiled runtime behavior, boundary behavior,
diagnostic instrumentation or allocation/retention logic from this PR. This is
an executable/source isolation proof, not a heap benchmark. RSS, peak memory and
allocation statistics were not measured. Cross-built targets were not executed
on foreign hardware; Linux AMD64 execution is the native smoke qualification.

Reproduce with a detached checkout of the exact main control and the final
candidate, an existing private cache, and the same toolchain:

```sh
GOCACHE=/tmp/wago-isolation-cache python3 evidence/isolation/verify.py \
  /path/to/main-control /path/to/candidate /tmp/wago-isolation-cgo0
GOCACHE=/tmp/wago-isolation-cache python3 evidence/isolation/verify.py \
  /path/to/main-control /path/to/candidate /tmp/wago-isolation-cgo1 \
  --native-only --cgo=1
just test native-compare
```

## Native TinyGo controls

The installed TinyGo 0.42.0 (Go 1.27.1, LLVM 22.1.4) built matched Linux AMD64
standard and minimal runtimes with one worker, tasks scheduler, conservative GC,
`-opt=z`, `-no-debug`, a fixed version stamp and process-local
`GOFLAGS=-buildvcs=false`. Both pairs are byte-identical: standard 7,810,008 bytes;
minimal 7,307,640 bytes. These are pre-postprocessing controls, not release-size
claims. [Hashes and build status](tinygo-native.json) retain the results.
Both native candidate runtimes also produce `fib(20)=6765`, recorded in
[TinyGo execution](tinygo-execution.json). Repeat the following in each matched
checkout, using `wago_runtime,wago_lean,wago_minimal` for the minimal control:

```sh
GOMAXPROCS=1 GOFLAGS=-buildvcs=false tinygo build -p=1 \
  -scheduler=tasks -gc=conservative -no-debug -opt=z -tags=wago_runtime \
  -ldflags='-X main.version=production-isolation-control' \
  -o /tmp/wago-tinygo-control ./cli/wago
```

TinyGo cross-platform binaries and foreign-hardware execution were not checked.
No new software or shared settings were installed or changed.

## Independent review and focused checks

An independent agent reviewed source commit
`b919acbb47f7f973c87ba8279c7de6b050679bb3` and recommended keeping the isolation
correction. It independently rehashed all ten retained native Go candidates and
all four TinyGo artifacts, checked distinctive diagnostic symbols, audited the
35-entry corrected symbol replay and confirmed production sources/modules are
unchanged. It separately ran the checked/profile diagnostic suite, including
the exclusion guard and inline-owner regressions, successfully in 0.153 seconds.
The reviewer found no remaining source/proof blocker for these configurations;
this is independent agent review, not maintainer approval.

The final local recipe, opt-in Staticcheck variants, formatting, actionlint and
99-file Markdown validation passed. The opted-in Linux ARM64 checked/profile
test binary cross-built successfully; it was not executed. The local focused
test output is retained in [local-checks.txt](local-checks.txt). These checks do
not substitute for the final published head's full CI, whose status is reported
separately on the PR.

## Outstanding review finding

The [P1 inline-owner discussion](https://github.com/wago-org/wago/pull/911#discussion_r4234742652)
was confirmed and corrected before this gating change. Capture retains logical
callee/PC/inline-parent metadata; containment uses the validated root caller's
physical ownership. Nested ancestry, imports, invalid parents/budgets, wrong
owners, changed caller context, and known/unknown location controls remain present.
The real inline capture accounts for 153 bytes (34 mapped, 119 raw), retaining
two unknown instructions and overall completion false. The discussion remains
unresolved/outdated; this document does not administratively resolve it or grant
reviewer approval. [Earlier reproduced failure and correction](../coverage/qualification-followup.md)
retain the before/after evidence.

The diagnostic still reports adapter-owned opaque bytes as static observations.
Production isolation does not waive the existing boundary hold or the user's
explicit merge prohibition. No host/guest ABI or production boundary code changes
are introduced by this PR.

Historical comparator timings and tool size remain attributed to measured source
`66dedc1decfe9061b60a3b42101400950915a7c3`. Later correctness fixes and this gate
were not rebenchmarked. Their diagnostic costs remain unmeasured; current-head
production build comparisons do not relabel old performance tables.
