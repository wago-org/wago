package embedded32

import "testing"

func TestScalarMemoryOperationLayouts(t *testing.T) {
	loads := []struct {
		op     ScalarLoadOp
		width  uint32
		words  uint8
		signed bool
	}{
		{ScalarI32Load, 4, 1, false}, {ScalarI32Load8S, 1, 1, true}, {ScalarI32Load8U, 1, 1, false},
		{ScalarI32Load16S, 2, 1, true}, {ScalarI32Load16U, 2, 1, false}, {ScalarI64Load, 8, 2, false},
		{ScalarI64Load8S, 1, 2, true}, {ScalarI64Load8U, 1, 2, false}, {ScalarI64Load16S, 2, 2, true},
		{ScalarI64Load16U, 2, 2, false}, {ScalarI64Load32S, 4, 2, true}, {ScalarI64Load32U, 4, 2, false},
		{ScalarF32Load, 4, 1, false}, {ScalarF64Load, 8, 2, false},
	}
	for i, tc := range loads {
		// The core opcode order places the four full-width loads before the
		// narrow integer loads, unlike ScalarLoadOp's representation order.
		loadOpcodes := [...]byte{0x28, 0x2c, 0x2d, 0x2e, 0x2f, 0x29, 0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x2a, 0x2b}
		wasmOpcode := loadOpcodes[i]
		mapped, mappedOK := ScalarLoadForWasmOpcode(wasmOpcode)
		if !mappedOK || mapped != tc.op {
			t.Fatalf("load opcode %#x: got (%d,%t), want (%d,true)", wasmOpcode, mapped, mappedOK, tc.op)
		}
		width, words, signed, ok := ScalarLoadInfo(tc.op)
		if !ok || width != tc.width || words != tc.words || signed != tc.signed {
			t.Fatalf("load %d: got (%d,%d,%t,%t), want (%d,%d,%t,true)", tc.op, width, words, signed, ok, tc.width, tc.words, tc.signed)
		}
	}
	if _, _, _, ok := ScalarLoadInfo(255); ok {
		t.Fatal("invalid scalar load accepted")
	}
	if _, ok := ScalarLoadForWasmOpcode(0x27); ok {
		t.Fatal("non-load Wasm opcode accepted")
	}

	stores := []struct {
		op    ScalarStoreOp
		width uint32
		words uint8
	}{
		{ScalarI32Store, 4, 1}, {ScalarI32Store8, 1, 1}, {ScalarI32Store16, 2, 1},
		{ScalarI64Store, 8, 2}, {ScalarI64Store8, 1, 2}, {ScalarI64Store16, 2, 2},
		{ScalarI64Store32, 4, 2}, {ScalarF32Store, 4, 1}, {ScalarF64Store, 8, 2},
	}
	for i, tc := range stores {
		storeOpcodes := [...]byte{0x36, 0x3a, 0x3b, 0x37, 0x3c, 0x3d, 0x3e, 0x38, 0x39}
		wasmOpcode := storeOpcodes[i]
		mapped, mappedOK := ScalarStoreForWasmOpcode(wasmOpcode)
		if !mappedOK || mapped != tc.op {
			t.Fatalf("store opcode %#x: got (%d,%t), want (%d,true)", wasmOpcode, mapped, mappedOK, tc.op)
		}
		width, words, ok := ScalarStoreInfo(tc.op)
		if !ok || width != tc.width || words != tc.words {
			t.Fatalf("store %d: got (%d,%d,%t), want (%d,%d,true)", tc.op, width, words, ok, tc.width, tc.words)
		}
	}
	if _, _, ok := ScalarStoreInfo(255); ok {
		t.Fatal("invalid scalar store accepted")
	}
	if _, ok := ScalarStoreForWasmOpcode(0x3f); ok {
		t.Fatal("non-store Wasm opcode accepted")
	}
}
