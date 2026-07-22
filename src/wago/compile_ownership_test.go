package wago

import "testing"

func TestCompileDoesNotRetainSourceForLinking(t *testing.T) {
	source := returningImportModule([]byte{0x60, 0x00, 0x01, 0x7f}, []byte{0x00, 0x10, 0x00, 0x0b})
	compiled, err := Compile(nil, source)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer compiled.Close()
	if !compiled.dynamicImports {
		t.Fatal("function imports were not compiled through dynamic dispatch")
	}
	if len(compiled.Code) == 0 || len(compiled.Entry) == 0 {
		t.Fatal("function-import module deferred native code generation")
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
