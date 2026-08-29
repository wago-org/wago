//go:build linux && amd64

package wago

import (
	"strings"
	"testing"
	"unsafe"
)

func TestExactFrameRootsRequireRuntimeDomainOnlyForDragline(t *testing.T) {
	roots := &compiledGCFrameRoots{}
	railshot := &Compiled{compiler: CompilerRailshot, validateMemo: &validateMemo{gcFrameRoots: roots}}
	if railshot.needsRuntimeGCCollectorDomain() {
		t.Fatal("Railshot exact frame roots unexpectedly selected the generic runtime GC domain")
	}
	dragline := &Compiled{compiler: CompilerDragline, validateMemo: &validateMemo{gcFrameRoots: roots}}
	if !dragline.needsRuntimeGCCollectorDomain() {
		t.Fatal("Dragline exact frame roots did not select the runtime GC domain")
	}
}

func TestGCFrameOffsetInternerSharesImmutableMaps(t *testing.T) {
	var interner gcFrameOffsetInterner
	source := []uint32{16, 24, 520}
	first := interner.intern(source, true)
	source[0] = 99
	second := interner.intern([]uint32{16, 24, 520}, true)
	if first[0] != 16 || second[0] != 16 || unsafe.SliceData(first) != unsafe.SliceData(second) {
		t.Fatalf("interned offsets first=%v second=%v pointers=%p/%p", first, second, unsafe.SliceData(first), unsafe.SliceData(second))
	}
	different := interner.intern([]uint32{16, 32, 520}, true)
	if unsafe.SliceData(first) == unsafe.SliceData(different) {
		t.Fatal("different root maps unexpectedly share storage")
	}
}

func TestSerialCompiledSealsExecutableMappingInPlace(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), fibWasm)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	original := unsafe.Pointer(&c.code[0])
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if len(c.codeCache.mem) == 0 || len(c.code) == 0 {
		t.Fatal("executable mapping or readable code view is empty")
	}
	mapped := unsafe.Pointer(&c.codeCache.mem[0])
	if got := unsafe.Pointer(&c.code[0]); got != mapped {
		t.Fatalf("compiled code still uses heap backing %p (mapped %p, original %p)", got, mapped, original)
	}
	if mapped != original {
		t.Fatalf("first Instantiate copied serial native code: mapped %p, original %p", mapped, original)
	}
	if _, err := c.MarshalBinary(); err != nil {
		t.Fatalf("marshal mapped code: %v", err)
	}
}

func TestCompiledCloseRejectsFutureInstantiate(t *testing.T) {
	c, err := Compile(nil, fibWasm)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := Instantiate(c, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Instantiate after Close error = %v, want closed", err)
	}
}

func TestCompiledCloseKeepsExistingInstanceAlive(t *testing.T) {
	c, err := Compile(nil, fibWasm)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	eng := in.eng
	jm := in.jm
	ar := in.ar
	serArgsPtr := unsafe.Pointer(nil)
	if len(in.serArgs) > 0 {
		serArgsPtr = unsafe.Pointer(&in.serArgs[0])
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close with live instance: %v", err)
	}
	if got := c.CodeSize(); got != 0 {
		t.Fatalf("CodeSize after Close with live instance = %d, want 0", got)
	}
	if _, err := Instantiate(c, InstantiateOptions{}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Instantiate after Close error = %v, want closed", err)
	}
	if in.eng != eng || in.jm != jm || in.ar != ar {
		t.Fatalf("live instance runtime objects changed after failed Instantiate")
	}
	if len(in.serArgs) > 0 && unsafe.Pointer(&in.serArgs[0]) != serArgsPtr {
		t.Fatalf("live instance argument buffer changed after failed Instantiate")
	}
	res, err := in.Invoke("fib", I32(10))
	if err != nil {
		t.Fatalf("Invoke after Compiled.Close: %v", err)
	}
	if got := AsI32(res[0]); got != 55 {
		t.Fatalf("fib(10) = %d, want 55", got)
	}
	in.Close()
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
