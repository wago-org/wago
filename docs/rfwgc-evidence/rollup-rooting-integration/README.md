# Rooting roll-up integration

Production integration pin: `45c83820fbc10ee31eaaa8437e12464955b2fb9d` on existing
`experiment/rfwgc-port`. This is scoped rooting qualification, not final combined
port approval or an INCLUDED label. Parent owns port-log dispositions and append
preservation. No collector-native, TinyGo/trampoline/toolchain, corpus, shared log,
submodule or audit-worker source edits were made by this worker.

## Commits and source selection

- `b46fefb2afe9f58ec3f64f2fba88a41b52a577d6`: exact main prerequisite hunks/tests
  #1025 custom-instruction call masks, #1029 tag-free catch maps, #1028 borrowed
  exception aggregate rejection, #1031 local-alias exception record retention.
  See `prerequisites.md` and its explicit path list.
- `866e6b5e7`: published PR1027 topic-only collector selection and original unit.
- `4ad440e86`: reconciled PR997/1026 root production, physical ownership and
  bounded adapter traversal, original source-engine regressions and stronger
  region/reentry/42->43 rewrite fixtures. See `pr997-1026.md` for hunk provenance.
- `27690e182`: two newly discovered EH integration repairs, **not attributed to
  published1027**: recognize required EH features on declared-tag modules, and
  mark AMD64/ARM64 catch-edge collector payload target flags. Real scalar import
  throw/catch/catch_all_ref, collection, alias-drop and trap-reuse regression.
- `ffece633e`: compiled scalar non-EH producer rejection control.
- `45c83820f`: separately authorized R9 H6-F2 immutable exact nullable-none/null
  global admission from collector-free owners, with object-bearing and rejection
  controls. Wago's matcher was already correct; see `immutable-none.md`.

Full hashes and source bases are in `sources.json`. PR997 provisionally uses
verified `e2736f0f37e614218ed7da7ff4040cf1697431c3`; supplied newer `421119d1d`
remains unavailable after the assessment's exhaustive search, and parent requested
its location. No equivalence/obsolescence claim. PR1026 `4101e75a3...` and PR1027
`29e59c641...` are topic-only against `27abf4470...`, never their full main ancestry.
No global baseline update or unrelated main fix was absorbed. #1022/#1003 remained
collector-owned; parent integrated them separately before the final pin.

## Preserved contracts

997's walker still authenticates physical code/context owners, bounds adapter
records, distinguishes foreign collector handles, traverses retained imports and
rejects missing maps. 1026 supplies unconditional domain discovery including RV64;
its older goto loop does not replace997. Existing activation-counter guard remains.
Synthetic1026 regression uses an actual retained compiled owner and bounded Engine
stack for both saved-return layouts and rewrites42 to43 in place.

Lifetime replay compile edits and retired public artifact rejection survive.
Source-engine tests remain source-only; no retired artifact fixture is restored.
Actual default/region allocation is asserted (directory state, fixed nursery,
collection counts), not inferred merely from passing a config. Foreign-domain
revisit adds region coverage alongside original stress/Tiny cases. Scalar EH
checks exact root-plan MaximumRoots, live payload count, value42, release after
consumption/trap and successful reuse. H6-F2 never exempts mutable or wider null
slots, ignores store identity, or relaxes exact type/owner lifetime validation.

RV64's catch representation was inspected separately: typed resultRefs/paramRefs
and SWAR reference classes already describe target payload carriers. No foreign
architecture offsets or flags were transplanted. TinyGo foreign-stack repair is
separate and must qualify jointly in the same final PR; these compact-root fixes
do not close that lane. R9 H2-F1 late container rooting remains parent-owned. Subsequent bounded
ownership report `d7e6f5dfc` confirms H6-F3 nested failed-start owner release on
this pin; parent assigned a separate ownership-graph repair worker. This rooting
matrix neither repairs nor qualifies that later fix.

## Gates and reproduction

`prepare.py` exports a Git archive plus only explicitly owned rooting paths;
`snapshot.json` records the final pin and per-path hashes. Final snapshot sources
match that committed pin. Earlier snapshot is recorded separately. This prevents
concurrent worktree edits from changing the final evidence. The original shared
preflight encountered an audit fixture disappearing during compilation; it was
not treated as a runtime failure. Archived runs explicitly select Go1.27.1 and
GOROOT to avoid mise trust/version ambiguity.

`validate.sh`: focused Wago GC/root/frame/host/EH/lifetime and artifact rejection,
AMD64/shared/frontend root/backend tests, and runtime stack/frame/host/native tests,
in ordinary, wagodebug, wago_regalloccheck. `diagnostics.sh` repeats relevant AMD64
checks with wago_codegenstats, making instrumentation-dependent assertions execute.
Raw final logs are in `final/`; exit logs and `results.json` retain pass/skip scope.
Final execution commands all exited zero. Top-level pass/skip counts:

| Lane | Ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Guest | 444/1 | 438/7 | 444/1 |
| Backend/shared/frontend | 83/26 | 81/28 | 85/26 |
| Runtime | 26/0 | 26/0 | 26/0 |
| AMD64 diagnostics | 82/0 | 80/2 | 83/0 |

All18 complete cross-recheck commands and all6 final-pin Wago cross-compiles
exited zero. All3,942 final snapshot src blobs match committed45c83820f.
Existing policy/profile skips remain explicit, never counted as passes. The known
unrelated interval-register diagnostic failures were not modified or waived.

`cross-compile.sh`: Linux ARM64 and RV64 Wago/backend/runtime binaries in all three
modes on the earlier EH-complete snapshot; final-pin Wago binaries repeated after
null-global changes. Backend/runtime source is identical across those pins.
The first cross driver was modified while running and ended with a shell parse
error after all18 compiler commands passed; `cross-driver-note.txt` records it.
`cross-recheck/` repeats the complete matrix from an immutable script copy.
Build outputs are deleted after compilation. No QEMU binary was available on PATH;
**cross-compilation is not ARM64/RV64 execution**. Darwin ARM64 and TinyGo execution
remain separate gates. Go test's printed durations are incidental runner output,
not performance samples; no benchmark or timing experiment was run.

Initial runtime/fixture failures are retained for the synthetic1026 code-size
assumption, required vs staged EH recognition and absent catch operand root, and
declaration-only region configuration. The explicit HostTrap correction is
documented in the implementation notes. Production EH
failures were repaired and regressions retained; fixture corrections did not
weaken root bounds, drop the rewrite or disable collections.

## Remaining combined gates

- Recover/verify supplied newer997 source, or obtain owner's final source decision.
- Final combined collector/parallel/full-root/nested-workspace/race/hardening,
  toolchain-backed conformance and corpus/oracle qualification.
- Actual ARM64 and RV64 execution; TinyGo foreign-stack interruption, forced GC,
  host reentry/cancel/trap/Close on the final combined source.
- Resolve parent-owned R9 H2 and confirmed H6F3 ownership findings and unrelated baseline diagnostics;
  no claim that this root walker proves either lifecycle obligation.
- Independent global-lease performance run, then consolidated three-Daybreak plus
  Claude fan, all fixes re-reviewed and zero unresolved findings on combined head.

READY TO MEASURE, by an independent leased worker after correctness workers drain.
Sensitive paths: native compiler call/root plans and catch targets, exact frame/
owner lookup and domain discovery, host activation/suspension, scalar EH import
selection, global attachment/admission. Proposed workloads: default/region/no-GC
controls; numeric host dispatch and compile costs; extern-heavy locals, shared-code
and foreign-domain relays, adapter depth, host reentry/cancel/trap, scalar EH
payload/alias transfers, immutable-null/object global linking and live collection.
Foreign-stack repair must be measured/qualified jointly, not silently excluded.
