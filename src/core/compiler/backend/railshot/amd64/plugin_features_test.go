//go:build linux && amd64 && !tinygo

package amd64

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"

	plugincodegen "github.com/wago-org/wago/codegen/amd64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func pluginCPUFeatureModule(t *testing.T, body []byte) *wasm.Module {
	t.Helper()
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("f")...), 0, 0)
	b := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 1, 0x0b}), wasmtest.Code(body))),
	)
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestUnusedPluginCPUFeatures(t *testing.T) {
	previous := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previous)
	for _, workers := range []int{1, 2} {
		for _, feature := range []plugincodegen.Features{plugincodegen.FeatureAVX2, plugincodegen.FeatureAVX512} {
			for _, profile := range []shared.AMD64Features{0, shared.AMD64KnownFeatures} {
				for _, tc := range []struct {
					name string
					body []byte
				}{
					{"unused", []byte{0x41, 1, 0x0b}},
					{"unreachable-call", []byte{0x00, 0x10, 0, 0x0b}},
				} {
					t.Run(fmt.Sprintf("%s/feature=%x/profile=%x/workers=%d", tc.name, feature, profile, workers), func(t *testing.T) {
						lowering := &plugincodegen.Lowering{Compatibility: plugincodegen.CompatibilityFullAccess, Features: feature, Emit: func(plugincodegen.Context) error {
							panic("unused plugin emitter called")
						}}
						cm, err := CompileModuleWith(pluginCPUFeatureModule(t, tc.body), CompileOptions{Workers: workers, AMD64FeaturesSet: true, AMD64Features: profile, CustomInstructions: map[uint32]CustomInstruction{0: {Codegen: lowering, ResultWidth: 32}}})
						if cm != nil && cm.CodeImage != nil {
							defer cm.CodeImage.Close()
						}
						if err != nil {
							t.Fatal(err)
						}
						if cm.RequiredAMD64Features != 0 || cm.RequiresAVX2 || cm.RequiresAVX512 {
							t.Fatalf("unused lowering acquired CPU requirements %#x, AVX2=%v AVX512=%v", cm.RequiredAMD64Features, cm.RequiresAVX2, cm.RequiresAVX512)
						}
					})
				}
			}
		}
	}
}

func TestExplicitPluginCPUFeatures(t *testing.T) {
	previous := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previous)
	for _, workers := range []int{1, 2} {
		for _, tc := range []struct {
			name     string
			selected shared.AMD64Features
			required plugincodegen.Features
			accept   bool
			want     shared.AMD64Features
		}{
			{"missing-avx2", shared.AMD64ModernBaseline, plugincodegen.FeatureAVX2, false, 0},
			{"avx2", shared.AMD64ModernBaseline | shared.AMD64AVX2, plugincodegen.FeatureAVX2, true, shared.AMD64AVX | shared.AMD64AVX2},
			{"missing-avx-state", shared.AMD64AVX2, plugincodegen.FeatureAVX2, false, 0},
			{"missing-avx512", shared.AMD64ModernBaseline, plugincodegen.FeatureAVX512, false, 0},
			{"avx512", shared.AMD64KnownFeatures, plugincodegen.FeatureAVX512, true, shared.AMD64AVX | shared.AMD64AVX2 | shared.AMD64AVX512},
			{"unknown", shared.AMD64KnownFeatures, 1 << 20, false, 0},
			{"ordinary", shared.AMD64ModernBaseline, 0, true, 0},
		} {
			t.Run(fmt.Sprintf("%s/workers=%d", tc.name, workers), func(t *testing.T) {
				m := pluginCPUFeatureModule(t, []byte{0x10, 0, 0x0b})
				var called atomic.Bool
				lowering := &plugincodegen.Lowering{Compatibility: plugincodegen.CompatibilityFullAccess, Features: tc.required, Emit: func(ctx plugincodegen.Context) error {
					called.Store(true)
					r := ctx.AllocGP()
					ctx.Encoder().MovImm32(r, 1)
					return ctx.OutputI32(r)
				}}
				cm, err := CompileModuleWith(m, CompileOptions{Workers: workers, AMD64FeaturesSet: true, AMD64Features: tc.selected, CustomInstructions: map[uint32]CustomInstruction{0: {Codegen: lowering, ResultWidth: 32}}})
				if cm != nil && cm.CodeImage != nil {
					defer cm.CodeImage.Close()
				}
				if (err == nil) != tc.accept || called.Load() != tc.accept {
					t.Fatalf("accepted=%v emitter=%v want=%v: %v", err == nil, called.Load(), tc.accept, err)
				}
				if tc.accept {
					if cm.RequiredAMD64Features != uint32(tc.want) || cm.RequiresAVX2 != tc.want.Has(shared.AMD64AVX2) || cm.RequiresAVX512 != tc.want.Has(shared.AMD64AVX512) {
						t.Fatalf("emitted requirements=%#x AVX2=%v AVX512=%v, want %#x", cm.RequiredAMD64Features, cm.RequiresAVX2, cm.RequiresAVX512, tc.want)
					}
				}
			})
		}
	}
}

func TestManagedPluginTracksActualVectorRequirements(t *testing.T) {
	for _, profile := range []shared.AMD64Features{0, shared.AMD64ModernBaseline, shared.AMD64ModernBaseline | shared.AMD64AVX2} {
		m := hostSyncModule(wasmtest.FuncType(nil, nil), []byte{0, 0x10, 0, 0x0b})
		lowering := &plugincodegen.Lowering{Compatibility: plugincodegen.CompatibilityManaged, Managed: func(ctx plugincodegen.ManagedContext) error {
			// No declared AVX2 bit: the managed operation must enforce its own ISA.
			r := ctx.ConstYMMRepeated128(0, 0)
			ctx.ReleaseVector(r)
			return nil
		}}
		cm, err := CompileModuleWith(m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: profile, CustomInstructions: map[uint32]CustomInstruction{0: {Codegen: lowering}}})
		if cm != nil && cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		if !profile.Has(shared.AMD64AVX2) {
			if err == nil {
				t.Fatalf("profile %x admitted managed AVX2", profile)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if !shared.AMD64Features(cm.RequiredAMD64Features).Has(shared.AMD64AVX | shared.AMD64AVX2) {
			t.Fatalf("lost managed requirements: %x", cm.RequiredAMD64Features)
		}
	}
}
