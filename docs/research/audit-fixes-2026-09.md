# September 2026 audit fixes

Base: `c46f2129e` (updated origin/main). Findings refer to the audit of
`01c7971d61`. Each numbered finding has one fix commit, combining its regression
coverage and implementation so each commit is independently reviewable.

## 1. Own authenticated artifact input

`UnmarshalBinary` now uses the streaming loader's owned RW code image and owned
metadata buffer. It rejects trailing input before publishing receiver state.
The input may be reused after the method returns. First instantiation seals the
same owned image, without a second native-code copy.

Validation: `go test ./src/wago -run
'Test(UnmarshalOwnsArtifactBytes|Compiled(ReadFrom|Sections)|MarshalRoundTrips)'
checks buffer reuse, section framing, and the owned-image path.

## 3. Snapshot low-level import bindings

Instantiation makes one shallow copy before validation. Binding, re-export
lookup, and resource teardown retain that same copy. Mutation or deletion in
caller maps after return cannot change an instance's bindings. Resource objects
remain shared by identity. Cost: one map copy per non-nil import set; no call-path
allocation is added.

Validation: import snapshot, function re-export, and table-close tests pass.
