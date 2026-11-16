# Published source provenance

[publication-provenance.json](publication-provenance.json) maps original canonical
commit IDs recorded during development to publicly published commits and their
identical Git trees. Publication recreated commit metadata, so commit IDs differ;
for every listed entry, both local and published commit trees were checked for
exact equality, and the published commit is an ancestor of the recorded verified
branch head. To inspect a recorded canonical runtime snapshot, use its matching
`published_commit` on GitHub rather than assuming the local ID is reachable there.

Historical benchmark records retain their original identifiers and hashes. Runtime
source, measurement-driver source, executable hashes and artifact hashes are
separate facts. This mapping covers published canonical commits, not every
local-only worker/harness experiment; it does not make an unpublished experimental
commit public. Driver/probe source and each run's scope are archived with the
corresponding evidence. Do not silently substitute newer production code when
reproducing an earlier run.

The manifest intentionally records already-published history, avoiding a
self-referential mapping of the commit that contains it. Later qualified source
may exist beyond the recorded head. Git-tree equality establishes source identity,
not performance equivalence across machines, toolchains or measurement protocols.
