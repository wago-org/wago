# Ready review qualification follow-up

The published diagnostic source d53357fed6de74daac02f462672741cd40b01d0d
entered the full CI profile after readiness. Run
[37994421170](https://github.com/wago-org/wago/actions/runs/37994421170)
reported two diagnostic-only Staticcheck findings: `compiledRevision` was unused
in ordinary/codegenstats builds (U1000), and the unsupported capture stub made
the CLI error condition always true (SA4023). Move the revision stamp and capture
dispatcher into their matching build variants. The disabled CLI regression checks
exit 2 and the support message without compilation, file access or execution.

The [inline review finding](https://github.com/wago-org/wago/pull/911#discussion_r4234742652)
is confirmed. Before the correction, a trusted bounded module using the established
backend inline fixture failed capture with:

```text
source region crosses/mismatches profile owner
```

Source ranges describe the logical callee; profile ranges describe the physical
caller. Preserve the existing source caller-frame table and each inline parent.
Resolve the root caller once in table order for ownership checks, retaining the
logical callee and PC in mapped changes and unknown locations. Containment and the
exhaustive mapped/raw byte partition still apply. No production backend, runtime,
host/guest boundary or executable instruction bytes change.

Caller metadata is capped at 128 frames. Parent indices must refer to earlier
frames; dangling source parents, forward/cyclic ancestry and oversized tables are
invalid. Snapshot pairs require exactly matching caller tables and region parent
indices for complete qualification. Changed or reordered contexts are conservatively
incomplete. Imported JSON still supplies unauthenticated structural metadata.

The corrected real capture accounts for 153 bytes: 34 mapped and 119 raw. It
retains two inline caller frames and both logical callees, including full function
indices shifted by an import. Its nine supported and two unknown instructions
leave overall completion false; raw completion is true. This is capture and
comparison validation, not execution of modified machine code.

Focused controls cover nested ancestry, incorrect physical owners, missing and
invalid parents, the frame budget, changed caller context, and preservation of
logical function/PC/parent in both known changes and unknown sites. Checked and
profile checked suites, vet, Staticcheck in ordinary/profile/codegenstats variants,
and an ARM64 checked/profile cross-build pass locally with one worker. Native
ARM64 capture/execution remains unqualified on this AMD64 host. The profile CLI
also retains its linked revision stamp and the existing 105-byte Fibonacci image.

Independent agent review found no source blocker: root resolution is bounded
linear work, ownership is anchored to the validated root, and changed context
cannot become complete. Its requested known-change ancestry assertion was added
and passed. Recommendation: keep the focused corrections, subject to exact-head
full CI qualification.

All previously published performance and tool-size figures refer to frozen
measured source 66dedc1decfe9061b60a3b42101400950915a7c3. These correctness
follow-ups were not rebenchmarked. Added ancestry validation and report metadata
costs, current tool binary size, peak/retained memory and RSS are unmeasured.
Historical tables are not current-head performance qualification. No merge or
issue closure is authorized by this follow-up.
