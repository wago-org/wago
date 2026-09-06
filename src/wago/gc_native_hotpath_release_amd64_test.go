//go:build linux && amd64 && !tinygo && !wago_guardpage && !wagodebug

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/runtime/gc/native"
)

// TestNativeGCHotPathDoesNotRecheckImmutableABI proves the production hot path
// consumes the trusted instantiation result rather than reloading version/count/
// stride guards on every operation. Dynamic handle/backing/object/type checks are
// still exercised by the allocation plus direct get.
func TestNativeGCHotPathDoesNotRecheckImmutableABI(t *testing.T) {
	compiled, err := compileStagedGCArray(stagedGCArrayNumericLocalBytes(t))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := instantiateCore(compiled, InstantiateOptions{GC: GCConfig{DisableCollection: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	collector := in.gc.NativeView()
	in.gcNativeView.Version = gc.NativeABIVersion + 1
	in.gcNativeView.LocalTypeCount = 0
	collector.Version = gc.NativeABIVersion + 1
	collector.HandleStride = gc.NativeHandleStride + 4
	if got, err := in.Invoke("get", 1, 0); err != nil || len(got) != 1 || got[0] != 0 {
		t.Fatalf("trusted immutable ABI hot path = %v, %v", got, err)
	}
	in.gcNativeView.Version = gc.NativeABIVersion
	in.gcNativeView.LocalTypeCount = uint32(len(in.c.Types))
	collector.Version = gc.NativeABIVersion
	collector.HandleStride = gc.NativeHandleStride
}
