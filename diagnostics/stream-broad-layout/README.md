# Compatibility-grouped broad source-layout experiment

Alternative source patches only, not active compiler code or a default build
change. Baseline source remains b5ab03f924e6b74ee3b558e34ad9cb32c9026acd. This
experiment uses a fixed prior-profile selection; it does not choose functions
or addresses from candidate results. No features, APIs, initializers or
assembly routines are removed. There is no performance or target claim.

## Fixed scope and ordering

The frozen selection contains 414 exact Wago-owned ELF symbols, mapped to
405 lexical declaration candidates. One declaration, Module.importCount, was
excluded because its file has a context-sensitive directive; no replacement
was selected. The final transformation moves 404 complete raw declarations
representing 413 selected symbols in eight packages and 16 compatibility
groups. Three generic declarations and 55 nested closures in 38 enclosing
declarations travel with their bodies. Seven selected noinline directives
travel verbatim. Source/debug positions necessarily change.

Groups require identical package, original header/build constraints, effective
platform suffix and compatible import aliases. The early variant uses descending
aggregate flat CPU cost, then smallest exact member symbol, within compatible
groups. Group files follow first appearance in that sorted stream. This is
compatibility-grouped profile order, not strict global cross-group rank.
The late control uses exactly the same group memberships, original filename
and offset order within each group, and group order by first original member.
It is a same-set late cluster, not a dispersed or randomized control.

Both alternatives are intended to use the same previously studied Linux/amd64
static internal ELF build placement, -T 0x402000 and -R 4096, with Go 1.27.1,
PGO off and all experimental streaming flags off. Those are experiment flags,
not new project defaults. They do not establish portability or source-level
40% compilation savings.

## Source gate and preserved failure

The generator preserves exact raw FuncDecl text including attached comments
and directives. It verifies whole-package function AST multisets, per-original-
file nonfunction and init order, original headers, effective platform suffixes,
and package import dependency sets. Initializers/types/variables do not move.
Bodyless functions, init, C/dot imports, and unsupported file-context directives
are excluded rather than rewritten. Closures and generic functions are not
renamed or independently extracted.

An initial isolated hot transformation failed the import dependency gate:
`gc/native` declares package `gc`, not `native`. Inferring its import name from
its path basename incorrectly omitted the required import. The failed source
was retained; no production build ran. The correction resolves actual package
clauses from the frozen module graph and GOROOT, preserving aliases and selector
text. The same invariants then passed in both alternatives. This was a generator
defect, not a relaxed comparison or changed hot set.

A supplemental binding audit checked all 808 moved declarations across both
arms for newly introduced file imports shadowing existing identifiers. Twelve
conservative flags were six named `bits:` struct-literal field keys duplicated
across arms, independently checked against regionLoopNode, integerReductionNode
and floatConstReg. These use the struct-field namespace, not math/bits. No
remaining collision was found. Resolver provenance pins the module graph,
Go/GOROOT identity and parsed package-clause inputs, including ignored package-
main files. Final frozen sources are hot14bb2dab and late-control b9d2a6ed.

Source review cleared static builds, followed by successful exact-placement API
observers in all three arms: 145 native comparisons against pristine reference
and mutual serialized-metadata comparisons, 22 HostCall callbacks per arm, and
trap/recovery plus PC-symbol checks. Private/debug metadata outside the
serializer is not covered. Full measurement records completed first attempt
(540 long, 2,900 cold, 435 RSS, 435 page runs), all oracle-verified; outcome
analysis is pending. No performance qualification is implied here. No PR or promotion is
proposed; existing negative experiments remain documented separately.

## Reproduction package

The supplied generator is a parameterized copy of the reviewed generator, not
the exact executed controller: private graph/cache paths become required
`LAYOUT_MODULE_GRAPH` and `GOMODCACHE` environment inputs. Use the matching
Go 1.27.1 toolchain/GOROOT and exact dependency module graph; run separately on
clean baseline worktrees for `hot` and `neutral`. Arguments are source root,
`frozen-symbols.json`, mode (`plan`, `hot`, `neutral`) and output manifest.
The sanitized symbol manifest preserves selection rows and profile/inventory
hashes but omits private artifact locators; its file hash therefore differs
from the original manifest hash recorded in transformation receipts.

Both alternative patches passed independent git apply --check against the
unchanged baseline source. Do not apply both to one worktree.


## Static code-generation caveats

All 9,463 named text symbols remain present. One function, stageFlushSpills,
changes size from 1,429 to 1,428 bytes in both variants. Bounded review found
local register-allocation changes in that function, reordered independent
field loads in tailLoopInlineCandidate, and a stack-temporary remap in the
late variant's InfosForArch. These are actual compiler-generated differences,
not merely relocated addresses. Conservative comparison initially reported
528 early / 901 late changed functions, predominantly RIP-relative referents.
Metadata referent equivalence was not exhaustively proven. Exact source
invariants, trusted official Go compilation and subsequent API/runtime gates
are the correctness boundary; no blanket instruction or executable-byte
identity claim is made. Any performance outcome is a combined source-
organization effect, not isolated pure layout.
