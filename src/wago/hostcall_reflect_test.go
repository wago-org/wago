//go:build !tinygo

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// TestSyncHostImportReflected binds a returning host import as a native Go
// function (adapted by reflection — standard Go only).
func TestSyncHostImportReflected(t *testing.T) {
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x10, 0x00, 0x0b} // local.get 0,1; call 0; end
	c := MustCompile(returningImportModule(sig, body))
	calls := 0
	in, err := Instantiate(c, Imports{"env.f": func(a, b int32) int32 { calls++; return a + b }})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke("g", I32(20), I32(22))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if AsI32(res[0]) != 42 {
		t.Fatalf("g(20,22) = %d, want 42", AsI32(res[0]))
	}
	if calls != 1 {
		t.Fatalf("host called %d times, want 1", calls)
	}
}

// TestSyncHostImportMemory verifies a host function can read the caller's linear
// memory through the leading HostModule parameter: g(addr) = env.peek(addr),
// where env.peek returns mem[addr].
func TestSyncHostImportMemory(t *testing.T) {
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b}           // local.get 0; call 0; end
	mem := wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})) // 1 memory, min 1 page
	c := MustCompile(returningImportModule(sig, body, mem))
	peek := func(m HostModule, addr int32) int32 { return int32(m.Memory()[addr]) }
	in, err := Instantiate(c, Imports{"env.f": peek})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	in.Memory().Bytes()[5] = 99
	res, err := in.Invoke("g", I32(5))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if AsI32(res[0]) != 99 {
		t.Fatalf("g(5) = %d, want 99 (mem[5])", AsI32(res[0]))
	}
}
