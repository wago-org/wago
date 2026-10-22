# Detached GuestStorage type queries

GuestStorage.DefinedType returned a shallow copy of live type metadata. Its
Supers, Params, Results and Fields slices still referred to execution storage.
A callback could edit its returned result descriptor and change the type used
by the host result check. The old runtime's later native typed-load check then
trapped; the host boundary had already accepted a wrong result type.

DefinedType now uses the same descriptor-clone helper as ModuleView.DefinedType.
All four slice fields have new storage. Their nested reference/type/field data
and the Array descriptor are value fields, so no further mutable references
remain. Nil, inactive and out-of-range guards are unchanged. Only a successful
metadata query pays for the copied descriptor/slices; ordinary field reads,
casts, object layout, code generation and native ABI are unchanged.

The public callback regression edits its query result, observes the active
required type, then supplies an incompatible ordinary struct token. The live
type must remain unchanged, and the host result boundary must report the
existing structural-type diagnostic. The companion test edits all four slices
and checks the source and later queries. Expired views must still reject.

A Go overlay restores only the original production file for the strict red.
Both new regressions fail in0.596 seconds: the live requirement changes, the
wrong result reaches the native cast trap, and slice edits reach the source.
Candidate complete src/wago normal/checked tests pass in22.598/23.875 seconds;
the complete checked benchmark module passes in34.416 seconds. Existing valid
host results, GC ownership/lifetimes, imported/shared types and callbacks remain
covered by those complete API checks. No speed or memory saving is claimed.

Initial complete API attempts failed because this fresh worktree lacked its
pinned official corpora (17.877/42.462 seconds). All three pinned gitlinks were
initialized before the successful reruns, with the pinned reference interpreter
provided. Original full setup logs remain locally; their commands/exits and the
successful raw results are retained here. No expectations or corpus goldens
were updated.

A separate audit considered the unused Exact flag in gcRefMatchesValueType.
The public compiler currently rejects exact indexed-reference signatures before
execution. That is not a demonstrated supported-input bug, so this change does
not alter that unrelated path or weaken its compile gate.

The fix and tests were prepared during the requested runtime proof audit. A
second source review confirms the clone helper detaches every slice-bearing
field in DefinedTypeDescriptor. Current native access checks remain in place;
this repair is a prerequisite to any later trusted typed-access proof.

Commands, source hashes, red output and full successful results are saved in
[guest-storage-type-isolation-results](guest-storage-type-isolation-results/identity.json).
