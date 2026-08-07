package wago

import (
	"bytes"
	"strings"
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
	if len(compiled.code) == 0 || len(compiled.Entry) == 0 {
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
	before := uintptr(unsafe.Pointer(&compiled.code[0]))
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer instance.Close()
	after := uintptr(unsafe.Pointer(&compiled.code[0]))
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
	if compiled.code != nil || compiled.codeCache.mem != nil {
		t.Fatal("Close retained an unsealed compiler code image")
	}
	if _, err := Instantiate(compiled, InstantiateOptions{}); err == nil {
		t.Fatal("Instantiate succeeded after Close")
	}
}

func TestCompiledCodeInspectionDoesNotExposeMutableStorage(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithFunctionWorkers(1), benchAddOneModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want := append([]byte(nil), compiled.code...)
	if got := compiled.CodeSize(); got != len(want) {
		t.Fatalf("CodeSize = %d, want %d", got, len(want))
	}
	var code bytes.Buffer
	n, err := compiled.WriteCodeTo(&code)
	if err != nil {
		t.Fatalf("WriteCodeTo: %v", err)
	}
	if n != int64(len(want)) || !bytes.Equal(code.Bytes(), want) {
		t.Fatalf("WriteCodeTo = (%d, %x), want (%d, %x)", n, code.Bytes(), len(want), want)
	}
	code.Bytes()[0] ^= 0xff
	if bytes.Equal(code.Bytes(), compiled.code) {
		t.Fatal("mutating diagnostic copy changed compiled code")
	}
	if err := compiled.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := compiled.CodeSize(); got != 0 {
		t.Fatalf("CodeSize after Close = %d, want 0", got)
	}
	if _, err := compiled.WriteCodeTo(&code); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("WriteCodeTo after Close error = %v, want closed", err)
	}
}

func TestCompiledDecodeReleasesReplacedCodeImage(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithFunctionWorkers(1), benchAddOneModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	oldCache := compiled.codeCache
	empty, err := (&Compiled{}).MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary empty: %v", err)
	}
	if err := compiled.UnmarshalBinary(empty); err != nil {
		t.Fatalf("UnmarshalBinary replacement: %v", err)
	}
	defer compiled.Close()
	oldCache.mu.Lock()
	oldClosed, oldMapped := oldCache.closed, len(oldCache.mem) != 0
	oldCache.mu.Unlock()
	if !oldClosed || oldMapped {
		t.Fatalf("replaced code cache = closed %v mapped %v, want closed and unmapped", oldClosed, oldMapped)
	}
	if compiled.CodeSize() != 0 {
		t.Fatalf("replacement CodeSize = %d, want 0", compiled.CodeSize())
	}
}

func TestCompiledDecodeRejectsReplacementWithLiveInstance(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithFunctionWorkers(1), benchAddOneModule())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer instance.Close()
	empty, err := (&Compiled{}).MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary empty: %v", err)
	}
	if err := compiled.UnmarshalBinary(empty); err == nil || !strings.Contains(err.Error(), "live instance") {
		t.Fatalf("UnmarshalBinary with live instance error = %v, want live-instance rejection", err)
	}
	result, err := instance.Invoke("f", I32(41))
	if err != nil {
		t.Fatalf("Invoke after rejected replacement: %v", err)
	}
	if got := AsI32(result[0]); got != 42 {
		t.Fatalf("f(41) after rejected replacement = %d, want 42", got)
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
