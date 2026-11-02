# ARM64 QEMU deadline discriminator

Source pin: `5847df30f1af15d10b743d90a24b1429b290d358`.
QEMU: Debian 10.0.13, package hash and emulator version in
`targets-5847df30f/source.json` and `arm64-version.log`.

**ARM64 guest commands FAIL in all three modes.** Each runs 75 top-level
families: 74 pass, `TestRRRecheckStorageOnlyReturnBehindDynamicReader` fails
in both `none` and `table` subcases. All guest builds pass. No failure is
converted to a skip, no original probe/deadline/assertion is changed, and no
adapted signal-selection run is credited. An earlier progress statement
inferred passes from log existence; it was withdrawn immediately after exit
codes were inspected. The summarizer checks exit codes, census and source
integrity and exits nonzero for this failed qualification.

The exact failure is `create native deadline timer: invalid argument` from
`src/core/runtime/interrupt_linux.go:589-595`. The original probe's first
storage-only call returns promptly, but its second call with a live one-second
context deadline reaches `timer_create` before guest execution. Same-binary
`-strace` records `timer_create(1,...) = -1 errno=22 (Invalid argument)`;
`arm64-timer-samebinary-strace.json` records command/exit, with the full raw log
beside it. Clock 1 is CLOCK_MONOTONIC. The event is 64 bytes, notify 4 is
SIGEV_THREAD_ID, target TID is syscall.Gettid(), and signal is the runtime's
selected compatible real-time signal. Runtime selection at lines 222-264
first considers a cached action, then scans signals 64 down to 35. There is
no existing environment override.

`timer-capability.go.txt` isolates that same struct and raw syscall without
Wago or the retirement repair. Its binaries create and immediately delete
unarmed timers; no signal is delivered. `timer-capability.json` records
bounded commands and raw logs record every errno:

| notify | signal 35: native AMD64 / QEMU ARM64 | signal 64: native AMD64 / QEMU ARM64 |
| --- | --- | --- |
| SIGEV_NONE (1) | 0 / 0 | 0 / 0 |
| SIGEV_SIGNAL (0) | 0 / 0 | 0 / EINVAL (22) |
| SIGEV_THREAD_ID (4) | 0 / 0 | 0 / EINVAL (22) |

This isolates a high-signal capability/mapping problem in this QEMU execution,
not blanket absence of SIGEV_THREAD_ID and not evidence of a native ARM64
failure. It does not prove timer firing, delivery, cancellation, or native
ARM64 semantics. No production interrupt code was changed.

The same unmodified ARM64 package binary independently passes
`TestGCAdmissionPublicDeadline` and
`TestCallDeadlineWhileCallbackOwnsAdmission`; raw log/command/exit are in
`arm64-existing-deadline-controls.*`. Those cases expire during admission,
before timer creation, so they do not contradict this failure or qualify the
missing native timer path. The new canceled-partial-admission and callback
retirement tests also pass in the original failed target commands.

**Open discriminator:** native Linux ARM64 execution of the exact pin and
original probes is required, or a separately reviewed qualified harness that
preserves production selection and POSIX deadline semantics. An adapted
harness cannot stand in for unmodified exact-source/native-platform evidence.
Parent owns platform coordination. No signal-selection harness is proposed
or added here; no expensive broad repeat was started after diagnosis.

RV64 commands pass all three modes, 48 top-level families each with no skips.
The RR3 probes and new retirement lifecycle tests have existing amd64/arm64
build constraints; RV64 is explicitly relevant controls, not execution of
those excluded tests. TinyGo is supported compatibility smoke only.
