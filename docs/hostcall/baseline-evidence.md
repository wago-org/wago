# Baseline boundary correctness probes

Base: `90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e`; Go 1.27.1, Linux amd64.

These probes test existing boundary obligations; they are not candidate gates. No benchmarks or performance measurements were run. Go test uses and removes its temporary build directories; no standalone binaries were produced.

## ordinary

```sh
go test -count=1 -run '^(TestHostCallPortalMayGrowStackCollectAndRecoverFromTrap|TestWideHostCallViewMayGrowStackCollectAndRecoverFromTrap|TestHostCallPortalMayBlockWhileOtherInstanceRuns|TestTypedI32HostCallbackMayGrowStackAndCollect|TestTypedI32HostCallbackMayBlockWhileOtherInstanceRuns|TestSyncHostImportInTableRunsIndirectly|TestImportedStartBadSignatureErrors|TestCrossInstanceHostDispatchUsesActiveCallee)$' ./src/wago
```

Exit status: 0.

```text
ok  	github.com/wago-org/wago/src/wago	0.008s
```

## wagodebug

```sh
go test -tags=wagodebug -count=1 -run '^(TestHostCallPortalMayGrowStackCollectAndRecoverFromTrap|TestWideHostCallViewMayGrowStackCollectAndRecoverFromTrap|TestHostCallPortalMayBlockWhileOtherInstanceRuns|TestTypedI32HostCallbackMayGrowStackAndCollect|TestTypedI32HostCallbackMayBlockWhileOtherInstanceRuns|TestSyncHostImportInTableRunsIndirectly|TestImportedStartBadSignatureErrors|TestCrossInstanceHostDispatchUsesActiveCallee)$' ./src/wago
```

Exit status: 0.

```text
ok  	github.com/wago-org/wago/src/wago	0.008s
```

## wago_regalloccheck

```sh
go test -tags=wago_regalloccheck -count=1 -run '^(TestHostCallPortalMayGrowStackCollectAndRecoverFromTrap|TestWideHostCallViewMayGrowStackCollectAndRecoverFromTrap|TestHostCallPortalMayBlockWhileOtherInstanceRuns|TestTypedI32HostCallbackMayGrowStackAndCollect|TestTypedI32HostCallbackMayBlockWhileOtherInstanceRuns|TestSyncHostImportInTableRunsIndirectly|TestImportedStartBadSignatureErrors|TestCrossInstanceHostDispatchUsesActiveCallee)$' ./src/wago
```

Exit status: 0.

```text
ok  	github.com/wago-org/wago/src/wago	0.008s
```

## Full root suite and test setup

A full `go test -count=1 ./...` initially failed because PowerShell lacked ICU support, the pinned spec submodules were absent, and a plugin installation encountered `text file busy`. The PowerShell tests passed with `DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1`; the plugin tests passed in isolated retries. After initializing the pinned submodules, staged tests correctly rejected the host WABT 1.0.42. The repository bootstrap supplied verified WABT 1.0.41, and its official interpreter bootstrap supplied the pinned Release 3 interpreter. The interpreter's intermediate build files were removed with `make clean` after the verified executable was copied into the worktree tool directory.

Prerequisites used the CI-declared packages `ocaml-nox`, `ocaml-dune`, and `menhir`. No source tests were skipped or changed to resolve these setup issues. Package serialization preserves the suite while avoiding the observed concurrent-package publication failures.

The final command passed with exit status **0** at the pinned base plus campaign documentation:

```sh
DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1 \
PATH="$PWD/.tools/wabt-1.0.41-linux-x64/bin:$PATH" \
WAGO_SPEC_INTERPRETER="$PWD/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm" \
WAGO_SPEC_INTERPRETER_REVISION=9d36019973201a19f9c9ebb0f10828b2fe2374aa \
go test -p=1 -count=1 ./...
```

The initial failures, intermediate tool-setup failures, successful isolated retries, final successful root suite, bootstrap verification and cleanup output are retained under `baseline-logs/`. These logs include normal test-run durations; no benchmark or performance timing workload was run. The full root suite here is ordinary mode; the earlier focused boundary selection passed all three required modes. Candidate mode/package/race qualification is recorded separately.
