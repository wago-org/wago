//go:build amd64

package amd64

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestLargeDeferredCapacityPreservesNativeCode(t *testing.T) {
	body := []byte{0, 0x20, 0}
	for i := 0; i < 512; i++ {
		body = append(body, 0x41, 1, 0x6a)
	}
	body = append(body, 0x0b)
	defs := make([]funcDef, 64)
	for i := range defs {
		defs[i] = funcDef{[]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body}
	}
	m := modFuncs(t, defs...)
	if moduleCodeCapacityAMD64(len(body)*len(defs), len(defs), currentCodegenPolicy()) < 256<<10 {
		t.Fatal("fixture misses reduced estimate")
	}
	mapped, err := CompileModuleWith(m, CompileOptions{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if mapped.CodeImage != nil {
		defer mapped.CodeImage.Close()
	}
	deferred, err := CompileModuleWith(m, CompileOptions{Workers: 1, DeferCodeMapping: true})
	if err != nil {
		t.Fatal(err)
	}
	if deferred.CodeImage != nil {
		defer deferred.CodeImage.Close()
	}
	if !bytes.Equal(mapped.Code, deferred.Code) {
		t.Fatal("heap capacity changed native instructions or layout")
	}
	for i := range mapped.Entry {
		if mapped.Entry[i] != deferred.Entry[i] || mapped.InternalEntry[i] != deferred.InternalEntry[i] {
			t.Fatalf("entry %d changed", i)
		}
	}
}
