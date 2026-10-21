//go:build arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCGeneralArrayLenEligibility(t *testing.T) {
	requireCompilerDiagnostics(t)
	for _, local := range []bool{false, true} {
		code := wasmtest.Code([]byte{0x20, 0, 0xfb, 15, 0x0b})
		if local {
			body := []byte{1, 1, 0x6a, 0x20, 0, 0x21, 1, 0x20, 1, 0xfb, 15, 0x0b}
			code = append(wasmtest.ULEB(uint32(len(body))), body...)
		}
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, []byte{0x60, 1, 0x6a, 1, 0x7f})),
			wasmtest.Section(3, wasmtest.Vec([]byte{1})),
			wasmtest.Section(10, wasmtest.Vec(code)),
		)
		m, err := wasm.DecodeModule(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := wasm.ValidateModule(m); err != nil {
			t.Fatal(err)
		}
		var stats ModuleStats
		c, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Stats: &stats})
		if err != nil {
			t.Fatal(err)
		}
		c.CodeImage.Close()
		if stats.Funcs[0].Peephole["gc-array-len-native"] != 1 {
			t.Fatalf("local=%v native array.len sites=%d, want 1", local, stats.Funcs[0].Peephole["gc-array-len-native"])
		}
	}
}

func TestGCGeneralArrayLenSharedModuleLeaf(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 1, 0x6a, 1, 0x7f})),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{0})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xfb, 15, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xfb, 15, 0x0b}))),
	)
	m, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := wasm.ValidateModule(m); err != nil {
		t.Fatal(err)
	}
	stub := emitGCObjectStub(nil)
	for _, workers := range []int{1, 2} {
		c, err := CompileModuleWith(m, CompileOptions{GCStructHelpers: true, GCArrayHelpers: true, Workers: workers, DeferCodeMapping: true})
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Count(c.Code, stub) != 1 || !bytes.HasSuffix(c.Code, stub) {
			t.Fatalf("workers%d: expected one module-owned resolver", workers)
		}
		stubAt, calls := len(c.Code)-len(stub), 0
		for at := 0; at < stubAt; at += 4 {
			op := binary.LittleEndian.Uint32(c.Code[at:])
			if op>>26 == 0x25 && at+int(int32(op<<6)>>6)*4 == stubAt {
				calls++
			}
		}
		if calls != 2 {
			t.Fatalf("workers%d: resolver calls=%d, want2", workers, calls)
		}
	}
}
