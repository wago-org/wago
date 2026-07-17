package wago

import "testing"

func TestCompileRetainsTransferredSourceWithoutCopy(t *testing.T) {
	source := returningImportModule([]byte{0x60, 0x00, 0x01, 0x7f}, []byte{0x00, 0x10, 0x00, 0x0b})
	compiled, err := Compile(nil, source)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(compiled.wasmBytes) != len(source) {
		t.Fatalf("retained source length = %d, want %d", len(compiled.wasmBytes), len(source))
	}
	if len(source) > 0 && &compiled.wasmBytes[0] != &source[0] {
		t.Fatal("Compile copied transferred source")
	}
}

func TestCompileMemoryPressureOnlyForLargeSources(t *testing.T) {
	if at, pressure := compileMemoryPressure((8 << 20) - 1); at != 0 || pressure != nil {
		t.Fatalf("small source pressure = (%d, %v), want disabled", at, pressure != nil)
	}
	if at, pressure := compileMemoryPressure(8 << 20); at != 0 || pressure == nil {
		t.Fatalf("large source pressure = (%d, %v), want (auto, enabled)", at, pressure != nil)
	}
}
