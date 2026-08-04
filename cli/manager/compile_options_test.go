package manager

import (
	"strings"
	"testing"

	compilecmd "github.com/wago-org/wago/cli/manager/commands/compile"
)

func TestStandaloneRuntimeOptionsValidateTargetKnobs(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	deferred := false
	gotDeferred, optimizations, err := standaloneRuntimeOptions("arm64", compilecmd.Options{
		DeferredBoundsChecking: &deferred,
		Optimizations:          map[string]bool{"three-op-sink": false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotDeferred || optimizations["three-op-sink"] {
		t.Fatalf("runtime options = deferred %v, optimizations %v", gotDeferred, optimizations)
	}

	_, _, err = standaloneRuntimeOptions("amd64", compilecmd.Options{
		Optimizations: map[string]bool{"three-op-sink": false},
	})
	if err == nil || !strings.Contains(err.Error(), `"three-op-sink" is unavailable on amd64`) {
		t.Fatalf("unsupported target knob error = %v", err)
	}
}
