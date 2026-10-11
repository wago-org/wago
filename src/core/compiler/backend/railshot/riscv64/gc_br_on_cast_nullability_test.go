//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRV64GCBrOnCastNullabilityExactAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		fail         bool
	}{
		{name: "success", source: rv64GCBrOnCastNullabilityHex},
		{name: "fail", source: rv64GCBrOnCastFailNullabilityHex, fail: true},
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
			if !admitRV64GCBrOnCastNullabilityWireKind(source, tc.fail) || !admitRV64GCBrOnCastNullabilityKind(m, tc.fail) {
				t.Fatal("exact nullability branch-cast module was rejected")
			}
			metadata, err := frontend.BuildGCTypeMetadata(m)
			if err != nil {
				t.Fatal(err)
			}
			opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCStructHelpers: true, SyncHostSlots: 4, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
			if tc.fail {
				opts.GCBrOnCastFailNullabilitySource = true
			} else {
				opts.GCBrOnCastNullabilitySource = true
			}
			compiled, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			wantCodeLen, wantCodeDigest, wantEntry := 2636, [sha256.Size]byte{0x73, 0xee, 0x12, 0x85, 0x73, 0x5d, 0x9f, 0x41, 0x41, 0x6e, 0xff, 0x0f, 0x1b, 0x1b, 0xfd, 0x79, 0xe6, 0xf3, 0x02, 0x93, 0x6a, 0xd0, 0x29, 0xbe, 0xb6, 0x22, 0xc6, 0x80, 0xc2, 0xfb, 0xd2, 0x4d}, []int{0, 784, 1568}
			if tc.fail {
				wantCodeLen, wantCodeDigest, wantEntry = 2460, [sha256.Size]byte{0x1d, 0x48, 0x18, 0x0e, 0x3b, 0xe3, 0xe2, 0xb0, 0x3f, 0x79, 0xcb, 0x9e, 0x82, 0xa2, 0xb1, 0xde, 0x5b, 0x2c, 0x30, 0x43, 0xdf, 0xf5, 0x27, 0x99, 0xa4, 0xea, 0x54, 0x2e, 0x38, 0x9a, 0x08, 0x53}, []int{0, 720, 1440}
			}
			if len(compiled.Code) != wantCodeLen || sha256.Sum256(compiled.Code) != wantCodeDigest || !slices.Equal(compiled.Entry, wantEntry) || !slices.Equal(compiled.InternalEntry, wantEntry) {
				t.Fatalf("compiled image identity changed: code=%d/%x entries=%v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
			}
			withoutWire := opts
			withoutWire.GCBrOnCastNullabilitySource = false
			withoutWire.GCBrOnCastFailNullabilitySource = false
			if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
				t.Fatal("decoded module compiled without exact wire provenance", err)
			}
			for _, at := range []int{0, len(source) / 2, len(source) - 1} {
				candidate := append([]byte(nil), source...)
				candidate[at] ^= 1
				if admitRV64GCBrOnCastNullabilityWireKind(candidate, tc.fail) {
					t.Fatalf("mutated wire admitted at byte %d", at)
				}
			}
		})
	}
}
