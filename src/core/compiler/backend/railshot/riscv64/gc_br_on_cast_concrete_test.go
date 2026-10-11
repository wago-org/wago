//go:build linux && !tinygo

package riscv64

import (
	"encoding/hex"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRV64GCBrOnCastConcreteExactAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		fail         bool
	}{
		{name: "success", source: rv64GCBrOnCastConcreteHex},
		{name: "fail", source: rv64GCBrOnCastFailConcreteHex, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, err := hex.DecodeString(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			if !admitRV64GCBrOnCastConcreteWireKind(source, tc.fail) || !admitRV64GCBrOnCastConcreteKind(m, tc.fail) {
				t.Fatal("exact concrete branch-cast module was rejected")
			}
			metadata, err := frontend.BuildGCTypeMetadata(m)
			if err != nil {
				t.Fatal(err)
			}
			opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCStructHelpers: true, SyncHostSlots: 4, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
			if tc.fail {
				opts.GCBrOnCastFailConcreteSource = true
			} else {
				opts.GCBrOnCastConcreteSource = true
			}
			if _, err := CompileModuleWith(m, opts); err != nil {
				t.Fatal(err)
			}
			withoutWire := opts
			withoutWire.GCBrOnCastConcreteSource = false
			withoutWire.GCBrOnCastFailConcreteSource = false
			if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
				t.Fatal("decoded module compiled without exact wire provenance", err)
			}
			for _, at := range []int{0, len(source) / 2, len(source) - 1} {
				candidate := append([]byte(nil), source...)
				candidate[at] ^= 1
				if admitRV64GCBrOnCastConcreteWireKind(candidate, tc.fail) {
					t.Fatalf("mutated wire admitted at byte %d", at)
				}
			}
		})
	}
}
