# ARM64 QEMU final diagnosis — bounded configuration repair for parent review

**Cause established separately for timers and cancellation.** On the exact original Debian QEMU `1:10.0.13+ds-0+deb13u1`, the default realtime-signal map does not support guest signal 64. A supported command-line configuration repairs all three originally failing assertions at source `08f511fac41a77f4c0f9046f88ebc78ad05a5da6`, in ordinary, wagodebug and wago_regalloccheck. No production repair, deadline change, signal-selection change or oracle relaxation was needed.

The historical combined result is still **216 PASS / 4 FAIL / 220 completed**. This directory is a separately pinned diagnosis, not a replacement ledger or final approval. Native audit `6a5d7272c8d35d1d660db80dc290f298682b04cd` remains separately attributable evidence.

## Exact cause and supported discriminator

QEMU's [v10.0.13 signal implementation](https://github.com/qemu/qemu/blob/v10.0.13/linux-user/signal.c) reserves host realtime signals for libc and QEMU, maps available signals upward from guest 32, and assigns an out-of-range host number to unmapped guest signals. With this binary's default mapping, guest 32–60 map to host 36–64; guest 61–64 are unavailable. `do_sigaction` nevertheless records/accepts an unsupported guest action. Thus Wago's compatible-handler scan legitimately selects guest 64 but cannot actually send it through this emulator configuration.

The supported `-t` / `QEMU_RTSIG_MAP` option is present in this exact binary's help and [upstream option parser](https://github.com/qemu/qemu/blob/v10.0.13/linux-user/main.c). The tested configuration is:

```text
qemu-aarch64 -cpu max -t '32 36 28,64 64 1' <test-binary> <exact-test-arguments>
```

It retains guest 32–59 at their default host 36–63 and maps **guest 64 to host 64**. Guest 60 is sacrificed; 61–63 remain unmapped. This is a limited lane configuration, not a complete Linux realtime-signal emulation fix. Do not apply it globally or infer coverage of the sacrificed signals. Full simultaneous guest realtime-signal support would require QEMU's still-missing signal multiplexing contract. No version upgrade was needed or tested.

Primary source snapshots and URLs are in `primary/`. The Debian source descriptor, Debian patch archive and patch-series audit are retained; its two patches touching these files concern static libc stubs and binfmt argument handling, not signal mapping or timer/queued-signal translation. The actual `.deb` SHA256 matches the original repair source manifest, and the executed QEMU binary is hashed in `provenance.json`.

## Timer failure: create path, not delivery

The unchanged native-audit timer control was cross-built with Go 1.27.3, CGO disabled, and executed under the same QEMU binary and `-cpu max`:

| Control | Default | Explicit map |
|---|---|---|
| SIGEV_NONE, signal 35 / 64 | both errno 0 | both errno 0 |
| SIGEV_SIGNAL, signal 35 / 64 | 0 / EINVAL | both errno 0 |
| SIGEV_THREAD_ID, signal 35 / 64 | 0 / EINVAL | both errno 0 |

QEMU `target_to_host_sigevent` translates `sigev_signo` through the signal map before host `timer_create`. Unsupported guest 64 becomes an invalid host signal. This is not evidence that SIGEV_THREAD_ID is generally unimplemented: the very same control works for 35 and for mapped 64. The control is unarmed and proves creation/deletion only. Existing native receipts show all six controls pass directly on ARM64.

In each current-source mode, both unchanged storage subtests reproduce `create native deadline timer: invalid argument` under the default map, then pass with the explicit map. The successful output retains `storageReturnBlocked=false secondAdmissionErr=<nil>`, result checks and cleanup. These runs are not evidence of timer expiration delivery.

## Guard failure: queued delivery, matching and unwind, independently

The guard has **no context deadline**. Every default and mapped guard trace has **zero `timer_create` calls**. The original 3-second cancellation assertion, GC progress requirement, GOMAXPROCS=1, native-leaf admission, loop input, and import selectors remain intact.

| Mode | Default send records, all EINVAL | Mapped signal-64 delivery records | Result default → mapped |
|---|---:|---:|---|
| ordinary | 1280 | 10 | fail-stop 86 → PASS 0 |
| wagodebug | 1536 | 4 | fail-stop 86 → PASS 0 |
| wago_regalloccheck | 1536 | 8 | fail-stop 86 → PASS 0 |

These are diagnostic record counts, not performance measurements or expected deterministic retry counts. Multithreaded syscall trace fragments can interleave.

Default: `rt_sigaction(64,...)` returns 0; every recorded `rt_tgsigqueueinfo(...,64,{si_code=SI_QUEUE,...})` returns EINVAL. No signal-64 delivery is recorded. The guard reaches its unchanged final cancellation timeout. This establishes an independent send failure before matching/unwind, not a consequence of timer creation.

Mapped: queued sends succeed; QEMU logs guest signal 64 with SI_QUEUE and the same full 64-bit `si_sigval` token as the sender. The offline audit checks token equality, including negative signed renderings. The guard returns `context.Canceled` and passes, including GC and cleanup. Production assembly is byte-identical: `interruptSigHandler` checks SI_QUEUE/SI_TIMER, cookie, saved PC range, saved X26/registered linear memory, active trap and full request token; it acknowledges the match, rewrites saved X9/PC, and lands in `nativeInterruptTrap`, which restores the foreign-stack continuation. Relevant exact-source files are retained in `inputs/`.

The trace directly observes send/delivery/token preservation. Successful unchanged assertions plus the preserved handler path support end-to-end matching and unwind; this is not an instruction-by-instruction ucontext trace. No residual matching/unwind failure is observed once delivery is enabled. Native all3 normal/race guard PASS remains independent corroboration.

## Source safety and reproducibility

Only immutable Git archives of 08f were exported into private cache. `audit.py` verifies all **7,505 non-doc tracked blobs** against their exact Git object IDs and SHA256, unchanged after execution. Every production file is untouched. The test-only stage7 declaration adapter is the existing QEMU fixture adapter, retained separately; its tests were not selected here.

The original guard timeout calls `t.Fatal`, which could run deferred Close against a live invocation. `failstop.patch` changes only the three guard timeout failure actions to print the original failure text and immediately `os.Exit(86)`, with fmt/os imports. No timeout, success branch or assertion is changed. A Go build overlay keeps the archived source itself unchanged. The same fail-stop binary is used for each default/mapped pair. No production instrumentation or interrupt-selection adapter is present. This deliberately preserves failed diagnosis without unsafe failure cleanup or subsequent tests in that process.

Exact build/run argv, cwd, environment, log hashes, binary hashes, ELF machine 183 and tool identities are retained. Only the two known-failing top-level test names execute; the storage test contains its original none/table leaves. Each invocation has a 30-second test timeout and 45-second process-group kill bound; builds have a 240-second bound. Internal test deadlines are untouched. No race rerun was made in QEMU; native race receipts remain separately pinned. No architecture-probe test ran here. Native `TestPlatformExecutionArchitecture` is still missing in the existing receipt; native host/ELF evidence proves architecture separately and must not be counted as that test.

Frozen `run.py`, `continue.py`, `continue2.py`, `continue3.py` document the actual sequence. Three early compile attempts failed because the initial archive omitted repository support directories; their logs are preserved. Missing immutable source directories were added, then all3 compiled and the bounded matrix completed. Do not rerun frozen drivers into this evidence directory. `commands.json` includes these failed setup attempts; they are not test outcomes.

`audit.py` is an offline verifier, including source integrity, command/log hashes, paired outcomes, timer matrix and token preservation. Raw logs are losslessly compressed; `compression.json` retains both hashes. `SHA256SUMS` covers committed artifacts except itself. Scratch binaries and source archives remain at the recorded private cache paths; they are not committed.

## Concrete disposition for the parent

`bounded-repair.py --out /absolute/new/output-directory` is a reviewable, mapped-only, six-process candidate rerun. It validates the exact QEMU and all3 fail-stop binary hashes, selects only the two known test names per mode, uses the proven map, preserves deadlines, writes new receipts, and stops on any unexpected result. It does not rewrite any historical result. It has **not been executed**; the equivalent traced paired matrix already passed. Its scope excludes the other historical 73 top-level repair tests and the added architecture probe. Parent approval owns any broader lane integration or gate disposition.

Recommended disposition: retain original FAIL, record **configuration cause established; bounded mapped repair PASS at 08f**, and review this explicit map for the narrowly scoped lane. Keep the native normal/race gates and add the missing explicit architecture probe in a separately approved run. Do not silently replace a QEMU gate with native coverage. Full-suite acceptance, broad signal coverage and final-port approval remain unresolved outside this task.

No verified production defect was found, so no production finding was triaged and no sibling audit was triggered. The configuration mismatch is in QEMU's guest/host signal availability; existing native evidence does not show a Wago production failure.

No PR, push, dispatch, subagent, benchmark or timing measurement was performed. Test framework durations and syscall timeout arguments in raw logs are incidental diagnostics, not performance results. This evidence-only commit cannot change shipped performance. Runtime cost of adopting the map and broader qualification cost are **unknown and unmeasured**; no performance estimate is claimed. Shared dirt, index and gitlinks are preserved; only this evidence directory is committed.
