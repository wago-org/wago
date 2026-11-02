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
