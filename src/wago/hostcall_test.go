package wago

import (
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

// returningImportModule builds a module whose func 0 is an import of type `sig`
// (env.f) and func 1 (exported "g") has body `body`. Optional extra sections
// (e.g. a memory) are appended before the export section.
func returningImportModule(sig, body []byte, extra ...[]byte) []byte {
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("f")...), 0x00, 0x00) // func, type 0
	fnBody := append(wasmtest.ULEB(uint32(len(body))), body...)
	secs := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
	}
	secs = append(secs, extra...)
	secs = append(secs,
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(fnBody)),
	)
	return wasmtest.Module(secs...)
}

// TestSyncHostImportSlotForm binds a returning host import as a reflection-free
// HostFunc (the TinyGo-safe form) and runs it through the public API.
func TestSyncHostImportSlotForm(t *testing.T) {
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b} // local.get 0; call 0; end
	c := MustCompile(returningImportModule(sig, body))
	in, err := Instantiate(c, Imports{"env.f": HostFunc(func(_ HostModule, p, r []uint64) { r[0] = p[0] * 3 })})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()
	res, err := in.Invoke("g", I32(7))
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if AsI32(res[0]) != 21 {
		t.Fatalf("g(7) = %d, want 21", AsI32(res[0]))
	}
}

func TestSyncHostImportRejectsNativeFunction(t *testing.T) {
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x00, 0x20, 0x00, 0x20, 0x01, 0x10, 0x00, 0x0b} // local.get 0,1; call 0; end
	c := MustCompile(returningImportModule(sig, body))
	_, err := Instantiate(c, Imports{"env.f": func(a, b int32) int32 { return a + b }})
	if err == nil {
		t.Fatal("Instantiate accepted a native Go function import; want HostFunc error")
	}
	if !strings.Contains(err.Error(), "wago.HostFunc") {
		t.Fatalf("Instantiate error = %q, want HostFunc guidance", err)
	}
}

func TestSyncHostImportMemorySlotForm(t *testing.T) {
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b}           // local.get 0; call 0; end
	mem := wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})) // 1 memory, min 1 page
	c := MustCompile(returningImportModule(sig, body, mem))
	in, err := Instantiate(c, Imports{"env.f": HostFunc(func(m HostModule, p, r []uint64) {
		r[0] = I32(int32(m.Memory()[AsI32(p[0])]))
	})})
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
