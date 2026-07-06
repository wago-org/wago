//go:build !tinygo

package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestVoidReflectedHostImportRunsOnce(t *testing.T) {
	c := MustCompile(voidI32ImportCallerModule())
	calls := 0
	in, err := Instantiate(c, Imports{"env.log": func(v int32) {
		calls++
		if v != 42 {
			t.Fatalf("param = %d, want 42", v)
		}
	}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if _, err := in.Invoke("g", I32(42)); err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if calls != 1 {
		t.Fatalf("host called %d times, want 1", calls)
	}
}

func TestImportedStartReflectedRuns(t *testing.T) {
	c := MustCompile(importedStartModule())
	calls := 0
	in, err := Instantiate(c, Imports{"env.start": func() { calls++ }})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if calls != 1 {
		t.Fatalf("start called %d times, want 1", calls)
	}
}

func TestImportedStartReflectedHostModuleRuns(t *testing.T) {
	c := MustCompile(importedStartModule())
	calls := 0
	in, err := Instantiate(c, Imports{"env.start": func(m HostModule) {
		calls++
		if m.Memory() == nil {
			t.Fatal("HostModule memory is nil")
		}
	}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	if calls != 1 {
		t.Fatalf("start called %d times, want 1", calls)
	}
}

func TestTypedNilReflectedHostImportRejected(t *testing.T) {
	c := MustCompile(voidI32ImportCallerModule())
	var f func(int32) = nil
	_, err := Instantiate(c, Imports{"env.log": f})
	if err == nil || !strings.Contains(err.Error(), "function is nil") {
		t.Fatalf("want typed nil function error, got %v", err)
	}
}

func TestVoidReflectedHostImportInTableRunsIndirectly(t *testing.T) {
	importSig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	localSig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x20, 0x00, 0x41, 0x00, 0x11, 0x00, 0x00, 0x41, 0x0b, 0x0b} // call_indirect; i32.const 11; end
	c := MustCompile(tableHostImportModuleWithLocal(importSig, localSig, body))
	calls := 0
	in, err := Instantiate(c, Imports{"env.f": func(v int32) {
		calls++
		if v != 8 {
			t.Fatalf("param = %d, want 8", v)
		}
	}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke("g", I32(8))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if AsI32(res[0]) != 11 || calls != 1 {
		t.Fatalf("g/calls = %d/%d, want 11/1", AsI32(res[0]), calls)
	}
}

func TestReflectedReturningHostImportInTableRunsIndirectly(t *testing.T) {
	sig := returningI32Sig()
	body := []byte{0x20, 0x00, 0x41, 0x00, 0x11, 0x00, 0x00, 0x0b}
	c := MustCompile(tableHostImportModule(sig, body))
	calls := 0
	in, err := Instantiate(c, Imports{"env.f": func(v int32) int32 {
		calls++
		return v + 1
	}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke("g", I32(41))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if AsI32(res[0]) != 42 || calls != 1 {
		t.Fatalf("g/calls = %d/%d, want 42/1", AsI32(res[0]), calls)
	}
}

func TestReflectedV128NamedArrayResult(t *testing.T) {
	type Vec [16]byte
	out := Vec{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	c := MustCompile(returningImportModule(v128ResultSig(), []byte{0x00, 0x10, 0x00, 0x0b})) // call 0; end
	in, err := Instantiate(c, Imports{"env.f": func() Vec { return out }})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke("g")
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if got := hostV128FromSlots(res[0], res[1]); got != V128(out) {
		t.Fatalf("v128 result = % x, want % x", got, V128(out))
	}
}
