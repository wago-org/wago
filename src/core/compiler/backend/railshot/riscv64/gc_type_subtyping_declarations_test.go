//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRV64GCTypeSubtypingDeclarationsAdmissionIsExact(t *testing.T) {
	wantLengths := []int{54, 65, 61, 39, 46, 73}
	wantDigests := []string{
		"aa9754e0665bda5f10ec77a3261759da4b462e813ecf9d0e12ec912acff996d6",
		"ddca4046060c72d14ed416806860b0512b8e34ae2d11555ed88ff8676f6d1871",
		"30ea9ab7a806640c081a4cd0bb68ecd9125f37524b6137f60af89a1c69df2839",
		"76131bcda4dc51168d7c55feabbc7bfb3489dc399b2bb3d0a89a05c56964b5cd",
		"2be8c2ca40f321f5ab956b191184d9b988e1f81963704f316f506bf18235bc9b",
		"ad59582ba55bea406e6c3f6a473bb1fbef90e66275bec4848972483b302ac8c9",
	}
	for wireIndex, pinned := range rv64GCTypeSubtypingDeclarationsWires {
		variant := uint8(wireIndex + 1)
		t.Run(strconv.Itoa(int(variant)), func(t *testing.T) {
			data := bytes.Clone(pinned)
			digest := fmt.Sprintf("%x", sha256.Sum256(data))
			if len(data) != wantLengths[wireIndex] || digest != wantDigests[wireIndex] || !AdmitGCTypeSubtypingDeclarationsSource(data) || GCTypeSubtypingDeclarationsSourceVariant(data) != variant {
				t.Fatalf("pinned declaration-only type-subtyping source was not admitted: len=%d want=%d digest=%s want=%s variant=%d", len(data), wantLengths[wireIndex], digest, wantDigests[wireIndex], GCTypeSubtypingDeclarationsSourceVariant(data))
			}
			if wireIndex >= 4 && (admitExactProductWireShape(data, []byte{1}) || !admitExactProductWireShapeWithRecGroups(data, []byte{1})) {
				t.Fatal("recursive wrapper escaped the exact-profile-only wire gate")
			}
			m, err := wasm.DecodeModule(data)
			if err != nil || admitRV64GCTypeSubtypingDeclarations(m) != variant {
				t.Fatal("pinned declaration-only type-subtyping module was not recognized", err)
			}
			metadata, err := frontend.BuildGCTypeMetadata(m)
			if err != nil {
				t.Fatal(err)
			}
			opts := CompileOptions{
				DeferCodeMapping:                  true,
				Interruptible:                     true,
				InvocationContext:                 true,
				ForeignTableCalls:                 true,
				GCTypeSubtypingDeclarationsSource: variant,
				Codegen: codegen.Options{Module: codegen.ModuleInfo{
					GCTypeDescs:   metadata.Descs,
					GCTypeLayouts: metadata.Layouts,
				}},
			}
			compiled, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(compiled.Code) != 0 || len(compiled.Entry) != 0 || len(compiled.InternalEntry) != 0 || compiled.CodeImage != nil {
				t.Fatalf("declaration-only product emitted executable state: code=%d entries=%v/%v image=%v", len(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
			}
			withoutWire := opts
			withoutWire.GCTypeSubtypingDeclarationsSource = 0
			if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
				t.Fatal("decoded product compiled without exact wire provenance", err)
			}
			wrongWire := opts
			wrongWire.GCTypeSubtypingDeclarationsSource = variant%uint8(len(rv64GCTypeSubtypingDeclarationsWires)) + 1
			if _, err = CompileModuleWith(m, wrongWire); !errors.Is(err, ErrUnsupported) {
				t.Fatal("decoded product compiled with mismatched exact wire provenance", err)
			}
			for i := range data {
				mutated := bytes.Clone(data)
				mutated[i] ^= 1
				if AdmitGCTypeSubtypingDeclarationsSource(mutated) {
					t.Fatalf("wire mutation %d remained admitted", i)
				}
			}
			m.Types[0].SubTypes[0].Final = !m.Types[0].SubTypes[0].Final
			if admitRV64GCTypeSubtypingDeclarations(m) != 0 {
				t.Fatal("decoded type mutation remained admitted")
			}
		})
	}
}
