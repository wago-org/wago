//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRV64GCBrOnCastAbstractExactAdmission(t *testing.T) {
	cases := []struct {
		name, hex string
		digest    [sha256.Size]byte
		fail      bool
	}{
		{"success", rv64GCBrOnCastAbstractHex, rv64GCBrOnCastAbstractSourceDigest, false},
		{"fail", rv64GCBrOnCastFailAbstractHex, rv64GCBrOnCastFailAbstractSourceDigest, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source, err := hex.DecodeString(tc.hex)
			if err != nil {
				t.Fatal(err)
			}
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			if sha256.Sum256(source) != tc.digest || admitRV64GCBrOnCastAbstractWireKind(source, tc.fail) == false || admitRV64GCBrOnCastAbstractKind(m, tc.fail) == false {
				t.Fatalf("exact abstract branch-cast module was rejected: len=%d digest=%x wire=%t decoded=%t", len(source), sha256.Sum256(source), admitRV64GCBrOnCastAbstractWireKind(source, tc.fail), admitRV64GCBrOnCastAbstractKind(m, tc.fail))
			}
			metadata, err := frontend.BuildGCTypeMetadata(m)
			if err != nil {
				t.Fatal(err)
			}
			opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCStructHelpers: true, GCArrayHelpers: true, SyncHostSlots: 4, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
			if tc.fail {
				opts.GCBrOnCastFailAbstractSource = true
			} else {
				opts.GCBrOnCastAbstractSource = true
			}
			compiled, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			wantLen, wantCodeDigest, wantEntry := 9004, [sha256.Size]byte{0x89, 0x4e, 0x26, 0x3b, 0x0e, 0x1a, 0xfe, 0xc0, 0x7e, 0xa9, 0x90, 0x8c, 0xe7, 0xc2, 0xed, 0xe4, 0x05, 0x6b, 0x3a, 0x65, 0x0a, 0x0d, 0x97, 0xd7, 0x71, 0xe9, 0xeb, 0x55, 0x49, 0x5c, 0xb3, 0x47}, []int{0, 608, 1856, 2688, 3680, 5008, 5984}
			if tc.fail {
				wantLen, wantCodeDigest, wantEntry = 9068, [sha256.Size]byte{0x00, 0x7c, 0x0d, 0x52, 0xd4, 0xa6, 0xde, 0x6f, 0x90, 0x73, 0x48, 0xb1, 0x78, 0xac, 0x7b, 0x25, 0x31, 0xb6, 0x82, 0x2a, 0x41, 0x4f, 0xfa, 0x73, 0x0c, 0x5d, 0x03, 0x20, 0xd7, 0x34, 0x5c, 0x27}, []int{0, 608, 1856, 2704, 3712, 5056, 6048}
			}
			if len(compiled.Code) != wantLen || sha256.Sum256(compiled.Code) != wantCodeDigest || !slices.Equal(compiled.Entry, wantEntry) || !slices.Equal(compiled.InternalEntry, wantEntry) {
				t.Fatalf("unexpected abstract branch-cast image: code=%d/%x entries=%v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
			}
			withoutWire := opts
			withoutWire.GCBrOnCastAbstractSource = false
			withoutWire.GCBrOnCastFailAbstractSource = false
			if _, err := CompileModuleWith(m, withoutWire); err == nil {
				t.Fatal("decoded branch-cast module compiled without exact wire provenance")
			}
			for i := range source {
				candidate := slices.Clone(source)
				candidate[i] ^= 1
				if admitRV64GCBrOnCastAbstractWireKind(candidate, tc.fail) {
					t.Fatalf("wire mutation %d retained admission", i)
				}
			}
			for name, mutate := range map[string]func(*wasm.Module){
				"type": func(v *wasm.Module) {
					v.Types[2].SubTypes[0].Comp.Array = wasm.NewFieldType(wasm.StoragePacked(wasm.PackI8), wasm.Var)
				},
				"table":   func(v *wasm.Module) { v.Tables[0].Type.Limits.Min++ },
				"element": func(v *wasm.Module) { v.Elements[0].Kind.Funcs[0]++ },
				"body":    func(v *wasm.Module) { v.Code[4].BodyBytes[6]++ },
				"export":  func(v *wasm.Module) { v.Exports[4].Name = "other" },
			} {
				t.Run(name, func(t *testing.T) {
					candidate, err := wasm.DecodeModule(slices.Clone(source))
					if err != nil {
						t.Fatal(err)
					}
					mutate(candidate)
					if admitRV64GCBrOnCastAbstractKind(candidate, tc.fail) {
						t.Fatal("decoded mutation retained abstract branch-cast admission")
					}
				})
			}
		})
	}
}
