package wago

import (
	"testing"
	"unsafe"
)

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

func TestSerialCompileSealsNativeCodeWithoutCopy(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithFunctionWorkers(1), benchAddOneModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer compiled.Close()
	if compiled.codeCache == nil || compiled.codeCache.flags&compiledCacheWritableCode == 0 {
		t.Fatal("serial compiler did not transfer its code image")
	}
	before := uintptr(unsafe.Pointer(&compiled.Code[0]))
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer instance.Close()
	after := uintptr(unsafe.Pointer(&compiled.Code[0]))
	if after != before {
		t.Fatalf("first Instantiate copied native code: %#x -> %#x", before, after)
	}
}

func TestSerialCompiledCloseBeforeInstantiateReleasesCodeImage(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithFunctionWorkers(1), benchAddOneModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if err := compiled.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if compiled.Code != nil || compiled.codeCache.mem != nil {
		t.Fatal("Close retained an unsealed compiler code image")
	}
	if _, err := Instantiate(compiled, InstantiateOptions{}); err == nil {
		t.Fatal("Instantiate succeeded after Close")
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
