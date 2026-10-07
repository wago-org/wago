//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

// Upstream's fresh-code host proofs authorize a different native/Go boundary.
// RV64 must keep ordinary framed park/resume even for the same numeric shape.
func TestRV64PublicHostOptimizationBoundary(t *testing.T) {
	source := rv64PublicHostModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	for _, artifact := range []bool{false, true} {
		c := rv64PublicStartCompile(t, source, artifact)
		view := c.executionView()
		if view.boundedHostSegments() || view.boundedNativeScalarLeaf() || view.nativeScalarLeafAllowed() || view.supportsGoHostDispatchTag() || integerHostContextMask != 0 {
			t.Fatal("RV64 acquired an unqualified host ABI proof", artifact)
		}
		for _, mask := range []uint{directHostSegmentsMask, nativeScalarLeafMask, nativeScalarBoundedMask, goHostDispatchTagMask} {
			marked := *view
			marked.InternalEntry = slices.Clone(view.InternalEntry)
			marked.InternalEntry[0] |= int(mask)
			if err := railshotValidateCompiled(&marked); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("foreign host proof reached RV64 admission", mask, err)
			}
			if err := marked.validateInternalEntries(true); err == nil {
				t.Fatal("serialized internal-entry marker accepted", mask)
			}
		}
		in, err := Instantiate(c, testImports("env.host", func(v int32) int32 { return v + 1 }))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = in.Close() })
		if in.eng.PreparedScalarHost() != nil || in.isolatedNativeScalarLeaf() || in.boundedHostSegments() {
			t.Fatal("RV64 selected a live host optimization")
		}
		prepared, err := in.WasmFunc("call")
		if err != nil {
			t.Fatal(err)
		}
		for range 3 {
			got, err := in.Invoke("call", 0xfeedface00000005)
			if err != nil || !slices.Equal(got, []uint64{6}) {
				t.Fatal("ordinary parked callback", got, err)
			}
			got, err = prepared.Invoke(0xfeedface00000005)
			if err != nil || !slices.Equal(got, []uint64{6}) {
				t.Fatal("prepared parked callback", got, err)
			}
		}
	}
	for _, name := range []string{"native-leaf-host", "direct-go-host-import"} {
		c, err := NewRuntimeConfig().WithOptimization(name, true).Compile(source)
		if c != nil {
			_ = c.Close()
		}
		if c != nil || err == nil {
			t.Fatal("unqualified optimization accepted", name, err)
		}
	}
}
