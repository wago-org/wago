//go:build linux && !tinygo && !wago_target_tinygo

package riscv64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestPublicNullableExternReferenceAnnotations(t *testing.T) {
	types := internalReferenceTypes{publicReferences: true}
	if !internalFunctionReferenceWithTypes(wasm.ExternRef, false, types) {
		t.Fatal("nullable external signature rejected")
	}
	for _, encoding := range [][]byte{{0x6f}, {0x63, 0x6f}} {
		block := wasm.ReaderFrom(encoding)
		if kind, err := internalReferenceBlockType(&block, false, types); err != nil || kind != -16 || block.HasNext() {
			t.Fatal("nullable external block", kind, err)
		}
		selectType := wasm.ReaderFrom(encoding)
		if kind, err := internalSelectType(&selectType, false, types); err != nil || kind != 0x70 || selectType.HasNext() {
			t.Fatal("nullable external select", kind, err)
		}
	}
	for _, encoding := range [][]byte{{0x64, 0x6f}, {0x63, 0}} {
		reader := wasm.ReaderFrom(encoding)
		if _, err := internalExplicitReferenceType(&reader, false, types); err == nil {
			t.Fatal("public nullable permission admitted an unproved type", encoding)
		}
	}
}
