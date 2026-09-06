package wago

import (
	"bytes"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/tests/wasmtest"
)

var compilerCompiledAllocationSink *Compiled

func TestCompilerCompiledStateUsesOneFixedOwnerAllocation(t *testing.T) {
	if !requireStandardGoTestRuntime(t) {
		return
	}
	allocs := testing.AllocsPerRun(100, func() {
		c := newCompilerCompiled(Compiled{})
		compilerCompiledAllocationSink = c
	})
	if allocs != 1 {
		t.Fatalf("compiler Compiled state allocations = %.0f, want one fixed owner", allocs)
	}

	c := newCompilerCompiled(Compiled{})
	base := uintptr(unsafe.Pointer(c.codeCache))
	state := compilerCompiledState{}
	if got, want := uintptr(unsafe.Pointer(c.validateMemo)), base+unsafe.Offsetof(state.validateMemo); got != want {
		t.Fatalf("validation memo address = %#x, want coallocated address %#x", got, want)
	}
	if got, want := uintptr(unsafe.Pointer(c.memoryDir)), base+unsafe.Offsetof(state.memoryDir); got != want {
		t.Fatalf("memory directory address = %#x, want coallocated address %#x", got, want)
	}
}

func TestPublicCompileFixedMetadataPreservesCodeIdentity(t *testing.T) {
	cfg := NewRuntimeConfig().WithFunctionWorkers(1)
	first, err := Compile(cfg, benchAddOneModule())
	if err != nil {
		t.Fatalf("first Compile: %v", err)
	}
	defer first.Close()
	second, err := Compile(cfg, benchAddOneModule())
	if err != nil {
		t.Fatalf("second Compile: %v", err)
	}
	defer second.Close()

	if !bytes.Equal(first.code, second.code) || !bytes.Equal(intSliceBytes(first.Entry), intSliceBytes(second.Entry)) || !bytes.Equal(intSliceBytes(first.InternalEntry), intSliceBytes(second.InternalEntry)) {
		t.Fatal("fixed-state allocation changed repeated public Compile code or entry tables")
	}
	if len(first.Entry) == 0 || len(first.Entry) != len(first.InternalEntry) || cap(first.Entry) != len(first.Entry) || cap(first.InternalEntry) != len(first.InternalEntry) {
		t.Fatalf("entry table shapes = %d/%d and %d/%d", len(first.Entry), cap(first.Entry), len(first.InternalEntry), cap(first.InternalEntry))
	}
	if got, want := uintptr(unsafe.Pointer(&first.InternalEntry[0])), uintptr(unsafe.Pointer(&first.Entry[0]))+uintptr(len(first.Entry))*unsafe.Sizeof(first.Entry[0]); got != want {
		t.Fatalf("internal entry table starts at %#x, want adjacent address %#x", got, want)
	}
	if first.Exports == nil || first.GlobalExports == nil {
		t.Fatal("public export maps changed from writable empty/non-empty maps")
	}
	if first.memoryDir == nil || !first.memoryDir.exactExports || first.memoryDir.exports != nil {
		t.Fatalf("private memory export directory = %+v, want exact lazy empty metadata", first.memoryDir)
	}

	memoryExportModule := wasmtest.Module(
		wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("memory", 2, 0))),
	)
	withMemoryExport, err := Compile(cfg, memoryExportModule)
	if err != nil {
		t.Fatalf("memory-export Compile: %v", err)
	}
	defer withMemoryExport.Close()
	if withMemoryExport.memoryDir == nil || !withMemoryExport.memoryDir.exactExports || len(withMemoryExport.memoryDir.exports) != 1 || withMemoryExport.memoryDir.exports["memory"] != 0 {
		t.Fatalf("materialized memory export directory = %+v, want memory -> 0", withMemoryExport.memoryDir)
	}
}

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

func TestCompilerPublicationReleasesHeapCodeBacking(t *testing.T) {
	// Parallel compilation emits a Go slice. Its grouped staging owner remains
	// reachable through the cache even after the public result is copied out.
	original := bytes.Repeat([]byte{0x5a}, 1<<20)
	staged := installCompilerCompiledFinalizer(newCompilerCompiled(Compiled{code: original}))
	published, err := publishCompilerCompiled(staged)
	if err != nil {
		t.Fatal(err)
	}
	defer published.Close()
	if staged.code != nil || staged.codeCache != nil || staged.validateMemo != nil {
		t.Fatal("compiler staging view retained code or metadata after publication")
	}
	snapshot := published.executionView()
	mapped := published.codeCache.mem
	if len(mapped) < len(original) || len(published.code) != len(original) || len(snapshot.code) != len(original) {
		t.Fatalf("image sizes: heap=%d public=%d execution=%d mapping=%d", len(original), len(published.code), len(snapshot.code), len(mapped))
	}
	if unsafe.SliceData(published.code) != unsafe.SliceData(mapped) || unsafe.SliceData(snapshot.code) != unsafe.SliceData(mapped) {
		t.Fatal("published views do not share the executable mapping")
	}
	if unsafe.SliceData(mapped) == unsafe.SliceData(original) {
		t.Fatal("publication retained heap-backed code")
	}
	var output bytes.Buffer
	if _, err := published.WriteCodeTo(&output); err != nil || !bytes.Equal(output.Bytes(), original) {
		t.Fatalf("code inspection after publication: %v", err)
	}
	if err := published.Close(); err != nil {
		t.Fatal(err)
	}
	if published.CodeSize() != 0 || published.codeCache.mem != nil {
		t.Fatal("Close retained code mapping")
	}
}
