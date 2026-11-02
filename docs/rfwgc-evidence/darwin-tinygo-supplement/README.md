# Native Darwin ARM64 TinyGo supplement — prepared, NOT_RUN

Execution source is **7e41fafbbcd737ed1edce11b94b43b4eda134dcf**. Existing Darwin
harness is **5fb47abb4b30472bd74751a5ea96b51d668e488a**, with run **41447853354**
reported active by the parent. Neither that harness, its running drivers, CI,
production source, nor other workers' files were edited. This directory is the
only owned deliverable. The supplement's committed HEAD must be reviewed and
supplied as the new harness SHA; the old harness SHA cannot identify these bytes.

**Feasibility:** source-only inspection and cross-build preparation have not found
a missing Darwin TinyGo production implementation at this pin. Provisioning and
ARM64-specific test coverage were missing and are supplied here. This is not a
claim that the production implementation is correct on Darwin. Native signals,
JIT mapping execution, task exit and stack lifecycle remain an execution gate.
No waiver, unsupported skip, Linux result, or cross-build closes that gate.

## Compiler and patch identity

The [official v0.42.0 release](https://github.com/tinygo-org/tinygo/releases/tag/v0.42.0)
provides `tinygo0.42.0.darwin-arm64.tar.gz` (163104695 bytes). The retained official
GitHub release response is `release.json`, asset ID 539147844. Download URL:
https://github.com/tinygo-org/tinygo/releases/download/v0.42.0/tinygo0.42.0.darwin-arm64.tar.gz

- Release archive SHA-256:
  `493da3585e66c5da677a4be39402faf2041b4e45a61eb456e4547eca753a544e`.
- Extracted native compiler SHA-256:
  `1b74ee7ef9841f01364823be1254833a96832e74076957c956d89872f705b7d2`.
- `toolchain.json` pins every patch/bootstrap input to the execution-source Git
  archive and records the exact pristine and patched runtime hashes. Both task
  exit renames and the registered foreign-stack integration are required.
- Local download matches the publisher's digest. No Homebrew rolling version,
  development compiler, secret, or locally rebuilt compiler is substituted.
- The [official macOS installation documentation](https://tinygo.org/getting-started/install/macos/)
  identifies a native ARM64 archive route. Its current compiler version is newer;
  this supplement deliberately uses the exact 0.42.0 release above.

`harness.py` checks archive and compiler identity before use, applies the exact
pinned source transformation to a private extracted toolchain, checks all four
result hashes, and runs the existing bootstrap negative controls. The offline
rename is byte-equivalent to `scripts/patch-tinygo-042.sh`; the foreign-stack
patcher and its negative-control script come from the immutable source archive.
No installed/shared toolchain is patched. Full effective tool-tree hashes are
retained before and after native execution, not just the changed files.

## Source-only platform audit and remaining correctness dependencies

`platform-audit.json` identifies inspected production and TinyGo files by hash.
The relevant implementation exists, with the following platform distinctions:

| Path | Source evidence | Native proof still required |
| --- | --- | --- |
| TinyGo `internal/task/task_threads.c` | Apple uses SIGIO rather than Linux SIGRTMIN+6; `sigaction` uses SA_RESTART, not SA_ONSTACK. Apple startup uses dispatch semaphores; detached pthread wrapper captures stack top and notifies exit. | Signal frames and interrupted registers must remain readable in the registered foreign mapping; startup/exit publication must work on the actual runner. |
| TinyGo `task_threads.go`, `gc_stack_threads.go`, installed `wago_foreign_stack.go` | GC holds activeTaskLock before signaling, pauses tasks, scans their normalized host spans, spills/scans the current task and scans registered mappings. Darwin uses pointer pthread IDs. | Apple pause/resume handshake, host-SP normalization, parked/resumed stacks and no stale host-stack scan after task exit. |
| Wago `trampoline_tinygo_arm64.go`, `engine_int_tinygo_arm64.go` | Darwin build tags exist. Save area is 176 bytes; host SP cell is stackTop−176. X19–X30 and low D8–D15 are saved. Entry/resume and prepared entry register the mapping and update saved host SP before foreign execution. | Darwin AAPCS64 indirect calls, nested host reentry, resume, unwind and actual prepared entry result. |
| Wago `mem_darwin.go`, TinyGo-specific `munmap_range_darwin_arm64_tinygo.go` / `madvise_darwin_arm64_tinygo.go` | MAP_JIT, writable copy then RX protection, Darwin-specific raw mapping lifecycle implementations. | Actual OS/JIT entitlement, permission/cache behavior and unaligned-unmap refusal. Mapping denial is failure, never a skip. |
| `interrupt_darwin_arm64.go` versus `cancellation_scheduler_tinygo.go` | Ordinary Go's Mach interruption path excludes TinyGo. TinyGo threads reports cooperative cancellation available. | Real infinite guest cancellation and Close must unwind within the test deadline; ordinary Go cancellation is not evidence for this path. |

The source audit does **not** prove the Apple kernel signal-frame contract or
JIT entitlement behavior. The native fixture is the next discriminator. If it
fails in one of these paths, retain the failure and report the exact production
path to the parent before any production edit. There is no production authority
in this task. Read-only sibling audit at the exact Slipstream and Starshine heads
in `platform-audit.json` found no corresponding TinyGo/foreign-stack integration
in their source languages; neither sibling was modified.

## Explicit supported and rejected API matrix

| TinyGo surface | Required behavior |
| --- | --- |
| Internal GC extern conversion identity, ownership, foreign/forged/closed rejection | Existing five `TestGCExternConversion*` tests run unchanged. Replacement/publication rollback and collector-first Close remain required. |
| Ordinary/null public externref global/table storage | Accepted through ordinary authenticated store paths; storage and lifetime checks remain required. |
| Foreign, forged, expired public tokens | Rejected without changing storage, for both local and owned globals. |
| Authenticated wrapper tokens | Keep the exact TinyGo classification regression assertions; no ordinary-Go layout assumption is substituted. |
| Opaque converted managed objects in host containers/imports/receipts/public tokens | Unsupported under TinyGo, including Darwin. Keep `gc_conversion_*_other.go` rejection paths and unchanged rejection assertions. The ordinary Darwin Go opaque-host port does not select these implementations. |
| Validation ordering | Immutable/closed errors precede invalid-token diagnostics; known opaque identities reject before ordinary-directory lookup; valid ordinary/null values still work. Eight exact ordering children required. |
| Serial mature evacuation | Supported with explicit region/evacuation policy; Workers 0/1 and shifts 6/12, relocation, cards/LOS and required pressure are tested. Workers >1 remains rejected. |
| Collector helper pools, native object-edge/reclaim callbacks, host receipt ownership | Not enabled or qualified by this TinyGo supplement. They are capability exclusions, never passing tests or blanket runtime skips. |

## Concrete harness and test adapters

The runner exports exact source with `git archive`, hashes the entire export,
then installs **test-only** adapters in that private export. Original archived
files remain byte-identical. No Go overlay support is assumed in TinyGo.

1. `gc_stage8_tinygo_guest_amd64_test.go` is copied under a neutral filename with
   only its build constraint changed. All Wasm bytes, movement assertions,
   positive moved-byte count, native-view generation checks, result 42, trap,
   forced GC, no-GC/default/region controls and Close assertions are unchanged.
2. `design_repair_unsupported_test.go` gets the same constraint-only treatment,
   retaining the TinyGo-specific rejection oracle, not the ordinary Darwin
   supported-host replacement used by the other harness.
3. `foreign_stack_arm64_test.go.in` supplies actual ARM64 machine-code fixtures.
   It does not retag the Linux x86 fixture. STLR/LDAR handshakes pair with host
   atomics; eight GCs finish before release. The suite checks return, trap-unwind,
   interruption trap, prepared integer 42, nested host reentry/park/resume with
   saved sentinel 151, and forced unaligned-thunk cleanup failure followed by GC
   and successful retry. No branch crosses the inserted host-resume rendezvous.
4. `cancellation_test.go.in` uses a directly encoded Wasm import-and-infinite-loop
   fixture. The import handshake proves entry, then GC and context cancellation
   or Close must complete. It cannot silently return through TinyGo's unavailable
   external-WAT helper. Each blocking operation is additionally bounded by the
   outer command deadline; no benchmark or performance sampling is performed.

`suite-plan.json` is the exact selector/required-name inventory. For **each** of
ordinary, wagodebug and wago_regalloccheck:

| Package | Ordinary / regalloc top-level passes | Debug top-level passes |
| --- | ---: | ---: |
| core/runtime | 7 | 7 |
| core/runtime/gc/native | 11 | 14 |
| wago | 15 | 15 |
| Total | 33 | 36 |

Total native requirement: **102 top-level passes**, plus required foreign-stack,
public-token and ordering children. Complete nested pass/fail/skip census is
retained. Debug includes existing metadata/backing, precommit, partial-copy and
committed-maintenance fault controls. Missing or duplicated required names,
unexpected top-level passes, any skip/failure, or known TinyGo early-return
messages fail the cell. Exit zero alone is insufficient. These focused gates
supplement the existing Linux 34-command lane and its evacuation/conversion/token
supplements; they do not claim to replay Linux's full runtime/CLI/serial-scheduler
matrix on Darwin. Existing normal CI and the parent's Darwin Go suite remain
separate required evidence.

## Native identity, bounds and workflow review

`workflow.proposal.diff` proposes a **new manual-only workflow**, not changes to
`.github/workflows/ci.yml`. The YAML is retained alongside the diff. Parent review
and publication are required; no workflow was installed, pushed or dispatched.
It has no job named `CI`, no push/PR trigger, and its distinct concurrency group
has cancel-in-progress false. Existing ARM groups and running Darwin drivers
remain untouched. Matrix fail-fast is false; max-parallel is one.

Configured ceiling: **3 × 110 + 3 = 333 job-minutes**. Each native lane has a
95-minute harness step: three builds × 20 minutes and three executions × 10
minutes, with remaining allowance for export/patch/provenance. Compiler download
has a five-minute network bound within a six-minute step; Go setup has ten minutes.
The job cap bounds combined setup/execution/upload, so a timed-out lane is
incomplete and cannot pass. Seven-day artifact retention includes failed/partial
logs, command receipts, source/tool manifests, exits and compiled binaries.

Native entry refuses Linux, wrong architecture, Rosetta, malformed/wrong harness
SHA, workflow/harness mismatch, dirty harness checkout, wrong archive/compiler,
patch drift, wrong Go version or non-ARM64 Mach-O. It uses explicit environment
values rather than inheriting GOOS, GOARCH, GOFLAGS or test overrides. It records
native OS/architecture, OS version, Xcode/SDK/Clang, Python, exact Go executable
hash/version, TinyGo version/hash, source/harness and every command. Runner SDK
and OS are attested infrastructure, not claimed hermetic pins.

Each package is built with `tinygo test -c -scheduler=threads -gc=conservative`,
then the **same hashed Mach-O** is executed directly with an exact selector. The
artifact's hash is checked again afterward. Original/effective source and tool
hashes are checked after the matrix. Independent package builds continue after
a failed build/run; any failure keeps the whole lane red. No success-only hidden
TinyGo gate, skip waiver, source mutation, or performance interpretation exists.

## Local validation and retained failures

`_local-validation-2/` records all three modes × three packages as **cross-only**
Mach-O builds, with exact argv, exits, binary hashes, ARM64 Mach-O headers and
required top-level test-name presence. No cross artifact is executed. The local
Linux compiler is separately hashed; it is not misrepresented as the Darwin
release compiler. Native runner rebuilds from the pinned Darwin archive.

`_negative-controls-final/` runs the existing pristine-apply/idempotence/drift/partial
patch negative suite in all three modes. Actual CLI calls reject wrong harness,
wrong archive and Linux native execution; retries cannot overwrite their output.
`_refuse-linux-*/` independently retains all-three-mode native-entry refusals.
The census controls reject missing/duplicate names, skips, failures, unexpected
top-level names and silent WAT early returns. `_unpatched-control/assessed-results.json` confirms pristine
Darwin runtime cross-link refusal for the required foreign-stack hook symbols.

Failures are retained: initial shell PATH reached an untrusted mise shim;
`_local-validation-1` wrongly decoded a Mach-O file as LLVM text because TinyGo
`test -c` ignores the `.ll` suffix for output format. The failure log and successful cross artifact
remain, with exact hashes in the final manifest. The unpatched-control parser initially missed TinyGo's exact "linker could not find symbol" diagnostic; original false results and the
corrected assessment of the same logs are both retained. Actionlint's unset shim version
and initial runner-context placement errors are retained; direct pinned
Actionlint 1.7.7 validates the corrected proposal.

Large private exports, release/tool cache and cross binaries remain locally in
this owned directory, excluded from the commit; their hashes and commands are
committed. No worker cache or export was deleted. Only explicit owned docs/scripts
and bounded evidence receipts are force-added. Other workers' dirt, index entries
and submodule/gitlink state remain theirs.

These harness/doc changes cannot affect shipped runtime performance. No benchmark,
measurement lease, timing sample or speedup claim is needed or made. Native
Darwin TinyGo remains **NOT_RUN**, and fullGCport remains gated on the reviewed
native result, not on this preparation report.

## Go package-discovery repair

Parent reported that the extracted `cache/tinygo/src` tree was discovered by
root Go package tooling and caused a Staticcheck crypto/tls compile error
(`undefined: netTLSConn`). This was harness workspace pollution, not a runtime
change. All owned directories containing exports, toolchains, caches or run
outputs were renamed with a leading underscore. Go excludes these directories
from `./...` discovery. `relocations.json` records original and new paths plus
same-filesystem inode identity and file counts. No file was deleted or rewritten
inside the moved trees. Historical command/provenance paths deliberately retain
the original spelling; resolve them through the relocation manifest. Parent
retains the failing log at `conversion-wrapper-cleanup/staticcheck.log`. The
private toolchain and source trees are never staged or committed.

After relocation, `_discovery-validation/results.json` records zero discovered
Go packages in all three modes and refusal of unsafe in-repository output paths.
`_negative-controls-final/` reruns the final CLI guards and bootstrap controls;
`_preparation-final/` reruns the final preparation entry point. The nine compiled
fixture artifacts remain valid: the relocation/output-path guard changes no
Go adapter or production bytes.
