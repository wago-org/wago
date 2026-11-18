//go:build amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestPackedTypeIDInlineDefinedChecks(t *testing.T) {
	savedShared, savedInline := gcSharedStubsEnabled, gcInlineCastsEnabled
	defer func() { gcSharedStubsEnabled, gcInlineCastsEnabled = savedShared, savedInline }()
	gcSharedStubsEnabled, gcInlineCastsEnabled = true, true

	var intervalLoad, identityGuard x64.Asm
	intervalLoad.Load64(R10, R8, gc.NativeViewSubtypeIntervalsOffset)
	identityGuard.CmpImm32Mem(R8, gc.NativeInstanceViewIdentityTypesOffset, 1)
	for _, tc := range []struct {
		name  string
		final bool
		test  bool
	}{
		{name: "final-cast", final: true},
		{name: "open-cast"},
		{name: "final-test", final: true, test: true},
		{name: "open-test", test: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decl := byte(0x50)
			if tc.final {
				decl = 0x4f
			}
			op, result := byte(22), byte(0x64)
			if tc.test {
				op, result = 20, 0x7f
			}
			functionType := []byte{0x60, 1, 0x6e, 1, result}
			if !tc.test {
				functionType = append(functionType, 0)
			}
			data := wasmtest.Module(
				wasmtest.Section(1, wasmtest.Vec(
					[]byte{decl, 0, 0x5f, 0},
					functionType,
				)),
				wasmtest.Section(3, wasmtest.Vec([]byte{1})),
				wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfb, op, 0, 0x0b}))),
			)
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			if err = wasm.ValidateModule(m); err != nil {
				t.Fatal(err)
			}
			meta, err := frontend.BuildGCTypeMetadata(m)
			if err != nil {
				t.Fatal(err)
			}
			var snapshotGuard x64.Asm
			snapshotGuard.CmpImm32Mem(R8, gc.NativeViewSubtypeIntervalCountOffset, int32(len(meta.Layouts)))
			cm, err := CompileModuleWith(m, CompileOptions{
				GCStructHelpers: true,
				GCArrayHelpers:  true,
				Codegen: codegen.Options{Module: codegen.ModuleInfo{
					GCTypeDescs: meta.Descs, GCTypeLayouts: meta.Layouts,
				}},
			})
			if cm != nil && cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := bytes.Contains(cm.Code, intervalLoad.B); got == tc.final {
				t.Fatalf("interval load present=%t for final=%t", got, tc.final)
			}
			if !bytes.Contains(cm.Code, identityGuard.B) {
				t.Fatal("baked target ID has no identity-map guard")
			}
			if got := bytes.Contains(cm.Code, snapshotGuard.B); got == tc.final {
				t.Fatalf("interval snapshot guard present=%t for final=%t", got, tc.final)
			}
			t.Logf("%s code bytes=%d", tc.name, len(cm.Code))
		})
	}
}
