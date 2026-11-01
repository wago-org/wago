go test -count=1 -timeout=25m -tags=wago_runtime\,wago_regalloccheck -run \^TestTinyGoSupportsCooperativeInterruption\$ ./cli/manager/internal/standalone
