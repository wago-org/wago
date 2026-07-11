package wagocli

import (
	"testing"

	"github.com/wago-org/wago"
)

func TestAutoHostsDoesNotOverrideRuntimeImports(t *testing.T) {
	provided := wago.Imports{
		"plugin.host": wago.HostFunc(func(wago.HostModule, []uint64, []uint64) {}),
	}
	hosts := autoHosts(&wago.Compiled{Imports: []string{
		"plugin.host",
		"env.fallback",
	}}, false, provided)
	if _, ok := hosts["plugin.host"]; ok {
		t.Fatal("auto host replaced a runtime-provided plugin import")
	}
	if _, ok := hosts["env.fallback"]; !ok {
		t.Fatal("auto host omitted an unprovided import")
	}
}
