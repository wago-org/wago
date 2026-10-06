//go:build arm64

package arm64

import (
	"crypto/sha256"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

// The fingerprint script compares ordinary and checked native code images.
func TestRegallocCheckEmissionFingerprint(t *testing.T) {
	cacheBody := []byte{0, 0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1, 0x20, 1, 0x42}
	cacheBody = append(cacheBody, wasmtest.SLEB64(0x123456789abcdef)...)
	cacheBody = append(cacheBody, 0x7e, 0x21, 1, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x0b)
	hints, err := scanBodyBytes(cacheBody[1:], 2, 0, 0)
	if err != nil || hints.loopIntConstCount == 0 {
		t.Fatalf("cache fixture lacks preload candidate: %v", err)
	}
	for _, test := range []struct {
		name string
		defs []funcDef
	}{
		{"loop-cache", []funcDef{{params: []wasm.ValType{wasm.I32, wasm.I64}, results: []wasm.ValType{wasm.I64}, body: cacheBody}}},
		{"scalar", []funcDef{{params: []wasm.ValType{wasm.I64}, results: []wasm.ValType{wasm.I64}, body: []byte{0, 0x20, 0, 0x42, 1, 0x7c, 0x0b}}}},
		{"control", []funcDef{{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x20, 0, 0x04, 0x7f, 0x41, 1, 0x05, 0x41, 2, 0x0b, 0x0b}}}},
		{"loop", []funcDef{{params: []wasm.ValType{wasm.I32}, results: []wasm.ValType{wasm.I32}, body: []byte{0, 0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0, 0x0b, 0x0b, 0x20, 0, 0x0b}}}},
		{"simd", []funcDef{{params: []wasm.ValType{wasm.V128}, results: []wasm.ValType{wasm.V128}, body: []byte{0, 0x20, 0, 0x20, 0, 0xfd, 0xae, 1, 0x0b}}}},
		{"call", []funcDef{
			{params: []wasm.ValType{wasm.I64}, results: []wasm.ValType{wasm.I64}, body: []byte{0, 0x20, 0, 0x10, 1, 0x0b}},
			{params: []wasm.ValType{wasm.I64}, results: []wasm.ValType{wasm.I64}, body: []byte{0, 0x20, 0, 0x42, 1, 0x7c, 0x0b}},
		}},
	} {
		for _, compact := range []bool{false, true} {
			cm, err := CompileModuleWith(modFuncs(t, test.defs...), CompileOptions{Workers: 1, CompactNative: compact})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("REGALLOC_CODE %s/compact=%t %x", test.name, compact, sha256.Sum256(cm.Code))
			if cm.CodeImage != nil {
				_ = cm.CodeImage.Close()
			}
		}
	}
}
