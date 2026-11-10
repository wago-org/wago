//go:build linux && amd64 && !tinygo && !wago_guardpage && wagodebug

package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestDebugNativeGCEntryGuardRejectsMutatedImmutableView(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), v128StructModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	// JobMemory basedata may still describe the last tenant until beginNativeEntry
	// rebinds this instance's captured context. The guard belongs after that bind,
	// not at the outer Invoke layer.
	in.jm.SetGCNativeViewPtr(0)
	if _, err := in.Invoke("default"); err != nil {
		t.Fatalf("debug entry guard rejected rebindable stale basedata: %v", err)
	}
	in.gcNativeView.Version = gc.NativeABIVersion + 1
	if _, err := in.Invoke("default"); err == nil || !strings.Contains(err.Error(), "native GC entry") {
		t.Fatalf("debug entry guard error = %v", err)
	}
}
