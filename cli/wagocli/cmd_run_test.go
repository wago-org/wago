package wagocli

import (
	"testing"

	"github.com/wago-org/wago"
)

func TestAutoHostsDoesNotOverrideRuntimeImports(t *testing.T) {
	provided := wago.Imports{
		"wasi_snapshot_preview1.fd_write": wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {}),
	}
	hosts := autoHosts(&wago.Compiled{Imports: []string{
		"wasi_snapshot_preview1.fd_write",
		"env.fallback",
	}}, false, provided)
	if _, ok := hosts["wasi_snapshot_preview1.fd_write"]; ok {
		t.Fatal("auto host replaced a runtime-provided WASI import")
	}
	if _, ok := hosts["env.fallback"]; !ok {
		t.Fatal("auto host omitted an unprovided import")
	}
}
