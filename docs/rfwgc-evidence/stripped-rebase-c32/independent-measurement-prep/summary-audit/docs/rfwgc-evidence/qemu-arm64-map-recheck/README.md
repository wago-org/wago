# QEMU ARM64 realtime-map independent recheck

## Verdict

**Map activation/scope: YES, narrowly. Whole 76-test qualification in all three modes: PASS.**

At source `08f511fac41a77f4c0f9046f88ebc78ad05a5da6`, exact Debian QEMU
`1:10.0.13+ds-0+deb13u1` with `-cpu max -t '32 36 28,64 64 1'` passed all
76 selected top-level repair tests in ordinary, wagodebug, and
wago_regalloccheck: **228/228 mode-test cells PASS, zero FAIL, zero SKIP**.
The original internal deadlines and the historical selection regex were
unchanged.

This is not full-port approval and does not establish general realtime-signal
coverage. The map preserves default guest 32–59 on host 36–63, maps guest 64
to host 64, and sacrifices guest 60; guest 61–63 also remain unavailable.
Coverage requiring guest signal 60 is therefore lost and was not represented
by this selected lane.

## Independent map review

The exact QEMU source accepts `-t`/`QEMU_RTSIG_MAP` as a supported option and
requires every target/host/count range to be a unique in-range realtime-signal
mapping. On this glibc host, signals 32–33 are libc-private, while QEMU's
`SIGRTMIN` is 34. This map uses host 36–64 and leaves 34–35 free. QEMU's
initialization then consumes those two free signals for its internal interrupt
and remapped guest SIGABRT paths. The map is one-to-one and passes those
initialization checks; it does not collide with libc's reserved signals or
QEMU's two reserved host signals.

The default-map failure mechanisms remain independently separated. The exact
diagnosis shows guest 64 translated out of range, causing `timer_create`
`EINVAL`; the no-timer guard separately shows every signal-64
`rt_tgsigqueueinfo` returning `EINVAL`. With the explicit map, all recorded
queued sends succeed and QEMU records SI_QUEUE delivery with the same complete
signed rendering of the 64-bit `si_sigval` token. The retained offline audit
checks delivered-token membership in the sent-token set. Mapped delivery
counts are ordinary 10, wagodebug 4, and wago_regalloccheck 8. The unchanged
guard then returns `context.Canceled` and passes, supporting delivery, match,
and unwind end to end without claiming an instruction-by-instruction ucontext
trace.

## Qualification construction and safety

`run.py` verified the exact QEMU and Go tool hashes and all 7,505 non-doc
source blobs before execution. It built fresh ARM64 binaries from the immutable
08f export. The overlay adds only the retained architecture probe and replaces
only `shared_entry_guard_arm64_test.go` with the diagnosis fail-stop copy. That
copy changes only the three timeout failure actions to immediate exit 86; it
does not change a timeout, success path, signal choice, assertion, or
production file. Because all three broad runs passed, no fail-stop action
executed.

The first qualification attempt stopped after ordinary passed because Python's
gzip wrapper rejected an unsupported `mtime` argument. The finite continuation
archived that exact completed log without rerunning ordinary, revalidated its
binary hash, and ran only the two unstarted modes. This packaging incident is
not a test outcome.

`census.json` records the exact 76 names. `results.json` retains argv, cwd,
binary/log hashes, passes, failures, and skips. `source-before.json` and
`source-after.json` both report 7,505 exact entries with no mismatch;
`tools-before.json` and `tools-after.json` agree; `process-drain.json` is empty.
The test binaries remain in the private cache paths recorded by
`binaries.json`. Native audit `6a5d7272c8d35d1d660db80dc290f298682b04cd`
remains separate and was not substituted for any QEMU gate.

No performance timing or benchmark was run. No production source, historical
ledger, native gate, PR, or remote branch was changed.
