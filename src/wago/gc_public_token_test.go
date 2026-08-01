//go:build (linux && (amd64 || arm64)) || (darwin && arm64)

package wago

import (
	"context"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func gcGenericPublicTokenModule() []byte {
	structType := []byte{0x5f, 0x01, 0x7f, 0x01}
	arrayType := []byte{0x5e, 0x7f, 0x01}
	structResult := []byte{0x60, 0x00, 0x01, 0x64, 0x00}
	arrayResult := []byte{0x60, 0x00, 0x01, 0x64, 0x01}
	newStruct := []byte{0x00, 0x41, 0x2a, 0xfb, 0x00, 0x00, 0x0b}
	newArray := []byte{0x00, 0x41, 0x07, 0x41, 0x02, 0xfb, 0x06, 0x01, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(structType, arrayType, structResult, arrayResult)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2), wasmtest.ULEB(3))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("new_struct", byte(wasm.ExternFunc), 0),
			wasmtest.ExportEntry("new_array", byte(wasm.ExternFunc), 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			append(wasmtest.ULEB(uint32(len(newStruct))), newStruct...),
			append(wasmtest.ULEB(uint32(len(newArray))), newArray...),
		)),
	)
}

func TestGenericGCResultsIssueBoundedHostTokens(t *testing.T) {
	base, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGenericPublicTokenModule())
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	if !base.usesGenericGCExecution() || base.genericGCFrameRoots() == nil {
		t.Fatal("generic result module lost exact collector/root admission")
	}
	for _, codec := range []bool{false, true} {
		mode := map[bool]string{false: "compiled", true: "codec"}[codec]
		t.Run(mode, func(t *testing.T) {
			compiled := base
			if codec {
				compiled = roundTripCompiled(t, base)
				defer compiled.Close()
			}
			for _, tc := range []struct {
				name string
				gc   GCConfig
			}{
				{name: "throughput", gc: GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, ForceMajorEveryMinor: true, VerifyAfterCollect: true}},
				{name: "tiny", gc: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, TinyStepBudget: 1, VerifyAfterCollect: true}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					in, err := Instantiate(compiled, InstantiateOptions{GC: tc.gc})
					if err != nil {
						t.Fatal(err)
					}
					structBits, err := in.Invoke("new_struct")
					if err != nil || len(structBits) != 1 || structBits[0]>>32 == 0 {
						in.Close()
						t.Fatalf("new_struct = %v, %v; want one opaque token", structBits, err)
					}
					structRef := ValueOf(ValAnyRef, structBits[0]).GCRef()
					if _, err := in.Invoke("new_array"); err == nil || !strings.Contains(err.Error(), "already has one live token") {
						in.Close()
						t.Fatalf("second live generic token error = %v", err)
					}
					if err := in.ReleaseGCRef(structRef); err != nil {
						in.Close()
						t.Fatal(err)
					}
					values, err := in.Call(context.Background(), "new_array")
					if err != nil || len(values) != 1 || values[0].Type() != ValAnyRef || values[0].GCRef().IsNull() || values[0].Bits()>>32 == 0 {
						in.Close()
						t.Fatalf("new_array = %v, %v; want typed opaque token", values, err)
					}
					arrayRef := values[0].GCRef()
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
					if err := in.ReleaseGCRef(arrayRef); err != nil {
						t.Fatalf("release after producer close: %v", err)
					}
					if err := in.ReleaseGCRef(arrayRef); err == nil || !strings.Contains(err.Error(), "stale") {
						t.Fatalf("stale generic token release = %v", err)
					}
				})
			}
		})
	}
}

func TestGenericGCResultTokensRetainExactSharedDomainOwner(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcGenericPublicTokenModule())
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	store := newReferenceStore(false)
	defer store.closeRuntime()
	config := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 128, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}
	first, err := instantiateCore(compiled, InstantiateOptions{GC: config, store: store})
	if err != nil {
		t.Fatal(err)
	}
	second, err := instantiateCore(compiled, InstantiateOptions{GC: config, store: store})
	if err != nil {
		first.Close()
		t.Fatal(err)
	}
	defer second.Close()
	if first.gc == nil || first.gc != second.gc || !store.ownsGCCollector(first.gc) {
		t.Fatal("generic token instances do not share one Runtime collector domain")
	}
	bits, err := first.Invoke("new_struct")
	if err != nil || len(bits) != 1 {
		first.Close()
		t.Fatalf("first new_struct = %v, %v", bits, err)
	}
	ref := ValueOf(ValAnyRef, bits[0]).GCRef()
	if err := second.ReleaseGCRef(ref); err == nil || !strings.Contains(err.Error(), "different producer") {
		first.Close()
		t.Fatalf("cross-producer release = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	secondBits, err := second.Invoke("new_array")
	if err != nil || len(secondBits) != 1 {
		t.Fatalf("second new_array after producer close = %v, %v", secondBits, err)
	}
	if err := second.ReleaseGCRef(ValueOf(ValAnyRef, secondBits[0]).GCRef()); err != nil {
		t.Fatal(err)
	}
	if err := first.ReleaseGCRef(ref); err != nil {
		t.Fatalf("release retained producer token: %v", err)
	}
}
