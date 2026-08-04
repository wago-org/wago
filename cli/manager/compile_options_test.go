package manager

import (
	"strings"
	"testing"

	compilecmd "github.com/wago-org/wago/cli/manager/commands/compile"
)

func TestStandaloneRuntimeOptionsValidateTargetKnobs(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	deferred := false
	gotDeferred, _, optimizations, err := standaloneRuntimeOptions("arm64", compilecmd.Options{
		DeferredBoundsChecking: &deferred,
		Optimizations:          map[string]bool{"three-op-sink": false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotDeferred || optimizations["three-op-sink"] {
		t.Fatalf("runtime options = deferred %v, optimizations %v", gotDeferred, optimizations)
	}

	_, _, _, err = standaloneRuntimeOptions("amd64", compilecmd.Options{
		Optimizations: map[string]bool{"three-op-sink": false},
	})
	if err == nil || !strings.Contains(err.Error(), `"three-op-sink" is unavailable on amd64`) {
		t.Fatalf("unsupported target knob error = %v", err)
	}
}

func TestStandaloneRuntimeOptionsResolveParallelPolicy(t *testing.T) {
	t.Setenv("WAGO_HOME", t.TempDir())
	_, workers, _, err := standaloneRuntimeOptions("amd64", compilecmd.Options{Parallel: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	if workers != 0 {
		t.Fatalf("adaptive function workers = %d, want 0", workers)
	}
	if _, _, _, err := standaloneRuntimeOptions("amd64", compilecmd.Options{Parallel: "many"}); err == nil {
		t.Fatal("invalid parallel policy was accepted")
	}
}

func TestStandaloneCoreFeaturesRejectUnknownVersion(t *testing.T) {
	if _, err := standaloneCoreFeatures("4"); err == nil {
		t.Fatal("unknown Core feature set was accepted")
	}
}
