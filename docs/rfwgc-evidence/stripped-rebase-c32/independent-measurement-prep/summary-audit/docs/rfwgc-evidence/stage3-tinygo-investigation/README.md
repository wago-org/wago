# TinyGo interruption qualification investigation

**Gate remains BLOCKED by a reproduced production integration defect.**
The cause is identified; it is not resolved by a toolchain downgrade or passing
retries. Production fix ownership stays with the parent/native worker.
See [DIAGNOSIS.md](DIAGNOSIS.md) for the exact fault chain and proposed fix scope.

## Findings and matched controls

| Probe | Baseline 9713878ab | Current d546a1af |
| --- | --- | --- |
| CI-pinned patched TinyGo 0.42.0, host ordinary | pass | pass |
| Same, host wagodebug | pass | pass |
| Same, host wago_regalloccheck | pass | pass |
| Unmodified 0.43.0 runner, process replay | abort at iteration 39 | abort at iteration 23 |
| Unmodified 0.43.0 runner, debugger | scanConservative SIGSEGV | scanConservative SIGSEGV |
| 0.42.0, force GC during guest | immediate abort; scanner fault in debugger | immediate abort; scanner fault in debugger |
| 0.42.0, force GC before guest starts | not run | 10 passes |

Iteration numbers are zero based, and replay stops on the first failure.
No probabilities or confidence bounds are inferred from these counts.
The current unchanged 0.42.0 binary also completed 500 process replays and 200
debugger replays without failing. That negative result does not contradict the
forced boundary fault. Three traced 0.42.0 runs confirm normal boundary order
and returned context.Canceled; instrumentation changes scheduling/allocation.

A second 0.42.0 forced probe was rebuilt with a fresh private XDG_CACHE_HOME.
This avoids shared TinyGo cache objects whose DWARF paths can refer to the
0.43.0 installation. It immediately aborts, and GDB identifies the same scanner fault in isolated-* logs. The first
matrix used the existing cache, as did the original qualification run.

All host test commands use Go 1.27.1, GOTOOLCHAIN=local, GOMAXPROCS=2,
GOFLAGS=-p=2 and `-count=1`. Host modes mean `-tags=wago_runtime`, then
`wago_runtime,wagodebug` or `wago_runtime,wago_regalloccheck`. They do **not**
propagate to subprocesses: the generator uses
`wago_standalone_generator,wago_target_tinygo`; the guest is always compiled
with `-scheduler=threads -gc=conservative -tags=wago_standalone`.
This is not evidence for wagodebug/regalloc-tagged TinyGo guest binaries.
No nursery region option is enabled. The full TinyGo CI matrix, other
architectures, and concurrent new port production edits are not qualified.

## Toolchain and source provenance

The [official 0.42.0 release](https://github.com/tinygo-org/tinygo/releases/tag/v0.42.0)
supports Go 1.27. Existing user-local Go 1.27.1 matches CI's pin; no additional
Go installation was necessary. The release API asset digest is archived in
release-042.json. Downloaded TinyGo archive SHA-256:
`b87688fa2e19cee7d813cad7fd7dadb71dff3198e47125aba66ba4af5e490438`.
CI's `scripts/patch-tinygo-042.sh` task-exit rename was applied, and hashes of
the resulting files, binary, archive and Go binary are in provenance.txt.
TinyGo installs only under `/home/dev/.local/share/wago-tinygo-investigation`.

GDB 16.3 and missing libraries were downloaded with `apt-get download` and
extracted with `dpkg-deb -x` in that same user-local directory. No system
package was installed. Package hashes are recorded. The original core handler
path `/usr/lib/systemd/systemd-coredump` and its storage directory are absent;
original-failure.jsonl is the complete original test-specific JSON evidence.
It contains no original fault address, and the failed executable was not
preserved. We do not claim recovery of that specific instruction history.

Both immutable exports were reused without edits, with all 8,007 current and
7,374 baseline tracked blobs verified before and after. Preserved runner
sources/native runtime copies live outside those exports. Relevant source
comparison is empty; both executables independently reproduce the failure.
Read-only sibling audit precedes triage and finds no comparable TinyGo surface.

## Reproduction

Run from this worktree. `provision.sh` documents the verified toolchain setup
and refuses to overwrite the installation. For the existing installation:

```sh
E=docs/rfwgc-evidence/stage3-tinygo-investigation
T=/home/dev/.local/share/wago-tinygo-investigation
# Lists exact binary locations and hashes:
cat "$E/artifact-manifest.json"
# Original runner, without any source modification:
python3 "$E/replay.py" "$T/artifacts/043-current-wagodebug/artifact-829998/runner" 100
python3 "$E/debug-replay.py" "$T/artifacts/043-current-wagodebug/artifact-829998/runner" 100
# Controlled pinned-toolchain repro:
python3 "$E/replay.py" "$T/artifacts/042-current-ordinary/artifact-811967-gc-during/runner" 1
python3 "$E/debug-replay.py" "$T/artifacts/042-current-ordinary/artifact-811967-gc-during/runner" 1
```

`matrix.sh` runs the six host modes and transparently saves each successful
build before the Go test removes its TempDir. Use a fresh evidence output copy
when rerunning, to avoid replacing these logs. Capture records contain exact
TinyGo arguments. `force-gc.py` generates a standalone runner against the
captured native runtime without changing that runtime. Modes `during` and
`before` differ in placement of explicit runtime.GC. `instrument.py` adds
boundary prints without changing assertions. Full generated runner texts are
also committed as *-runner.go.txt. All replay limits/timeouts are correctness
bounds; no benchmark, intentional timing, or performance interpretation occurs.

GDB must pass TinyGo's GC signal SIG41 without stopping. The initial
exploratory debug-replay-043-current.jsonl stops at that expected signal and
is **not** crash evidence. The subsequent *-fault.jsonl and .txt files contain
SIGSEGV evidence. The first gdb-first.txt is a successful normal exit followed
by a harmless attempt to inspect nonexistent registers. These attempts are
preserved, not silently relabeled as failures.

## Handoff

No production fix is authorized within this worker's owned scope. The parent
must implement and validate GC-aware foreign-stack integration or another
correct entry strategy before the aggregate gate can close. Calling the defect
pre-existing does not waive it. No PR/push, worktree switch, subdelegation,
benchmark, production edit, or sibling edit occurred. Evidence-only changes
cannot affect production performance. Original dirty corpus/log/submodules and
other workers' files were preserved.
