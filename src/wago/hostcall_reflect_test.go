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

func TestSyncHostImportReflectedV128(t *testing.T) {
	inVec := V128{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	outVec := V128{15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0}
	sig := wasmtest.FuncType([]wasm.ValType{wasm.V128, wasm.I32}, []wasm.ValType{wasm.V128})
	body := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x10, 0x00, 0x0b} // local.get 0,1; call 0; end
	c := MustCompile(returningImportModule(sig, body))
	calls := 0
	host := func(v V128, tag int32) V128 {
		calls++
		if v != inVec || tag != 123 {
			t.Fatalf("host got (%x,%d), want (%x,123)", v, tag, inVec)
		}
		return outVec
	}
	in, err := Instantiate(c, Imports{"env.f": host})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	lo, hi := hostV128Slots(inVec)
	res, err := in.Invoke("g", lo, hi, I32(123))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if calls != 1 {
		t.Fatalf("host called %d times, want 1", calls)
	}
	if got := hostV128FromSlots(res[0], res[1]); got != outVec {
		t.Fatalf("v128 result = % x, want % x", got, outVec)
	}
}
