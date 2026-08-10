//go:build linux && amd64 && !tinygo && !wago_guardpage && wagodebug

package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc"
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
	in.gcNativeView.Version = gc.NativeABIVersion + 1
	if _, err := in.Invoke("default"); err == nil || !strings.Contains(err.Error(), "native GC entry") {
		t.Fatalf("debug entry guard error = %v", err)
	}
}
