# Descriptor and Tiny publication prerequisites

Integrated the verified published #1003 extent-validation topic from
9666ec48e18cb2f7a77c188dc52f093770859f26 and the landed #1022 guard from
703b47d3e40b55837279363a2917c03d37a74ea4. Starting head:
27690e182842142c79b8cc598d3b572468004cf5. The owner-supplied #1003
11bcf23fd remains unavailable; no equivalence claim is made. The historical
abstract-array fusion topic is not imported. Only the descriptor hunk and its
original regression are taken from #1003: a broad branch diff would undo #1022.

refDescPtrBytes validates the space tag, header size, and widened offset-plus-size
before slicing the appropriate backing. Original tests cover nursery, survivor,
old, large and Tiny spaces; added real region nursery/old/large controls retain
those checks and restore a valid reference afterward. validateStoredRef rejects
the shared Tiny scan cursor as pending reclamation only in tinySweep; the original
partially scanned gray-array publication regression is retained unchanged.

## Validation

All passed on the exact production bytes in this commit:

- `go test ./src/core/runtime/gc/native`
- `go test -tags wagodebug ./src/core/runtime/gc/native`
- `go test -tags wago_regalloccheck ./src/core/runtime/gc/native`

After adding only the three region fixture rows, the expanded
`TestRefDescRejectsInvalidObjectExtent` passed with `-count=1` in each mode.
No benchmark or timing acquisition was run. Go test duration output is not a
performance result. Source checksums are retained alongside this report.

Read-only sibling assessments are in ../rollup-rooting-assessment/README.md
(descriptor ownership) and ../rollup-1030-1019/README.md (Tiny cursor): Slipstream
has different address-indexed publication/marking, while Starshine's inspected
surface is model/codec/validation. Neither supersedes these Wago checks.

#1003 is integrated pending combined final gates, not yet final INCLUDED.
Full-root, final-head integration, independent measurement and four-reviewer fan
remain open. #1022 is a tight GC prerequisite, not a newly tabled PR disposition.

The 1019 commit e23657e6a85267106e39d4e1b23b523342a057d9 also captured 32
concurrently staged r9-features-protocol evidence files. Their ownership belongs
to the R9 assessment finalized at 7c59a7c8444f91213e107150f0c2f730e43d11a6;
this is provenance reconciliation, not an additional 1019 production claim.
Shared history has not been rewritten.

READY TO MEASURE after qualification and worker drain, by an independent holder
of the global lease. Sensitive paths: descriptor lookup/casts and Tiny reference
publication. Workloads: matched default/region cast-heavy graphs, Tiny gray-array
publication, and no-GC controls. No PRs were opened, pushed or closed.
