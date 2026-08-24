//go:build arm64

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func arm64HostCallModule(importSig, localSig, body []byte, extra ...[]byte) []byte {
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("f")...), 0x00, 0x00) // func, type 0
	fnBody := append(wasmtest.ULEB(uint32(len(body))), body...)
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(importSig, localSig)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
	}
	sections = append(sections, extra...)
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(fnBody)),
	)
	return wasmtest.Module(sections...)
}

// TestARM64HostCallPreservesExtendedPinnedLocals keeps eight integer locals hot
// across a returning host call. The arm64 pin pool assigns the first five to
// X19-X23 and may use X9-X11 for the next three. Host-call setup also uses
// X9-X11 as fixed scratch, so those locals must be homed before scratch setup.
// Regression for #490.
func TestARM64HostCallPreservesExtendedPinnedLocals(t *testing.T) {
	sig := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	body := []byte{
		0x01, 0x08, 0x7f, // one local run: eight i32 locals
	}
	for i := byte(0); i < 8; i++ {
		body = append(body,
			0x41, 10+i, // i32.const 10+i
			0x21, i, // local.set i
		)
	}
	body = append(body,
		0x10, 0x00, // call env.f
		0x1a,       // drop host result
		0x20, 0x00, // local.get 0
	)
	for i := byte(1); i < 8; i++ {
		body = append(body,
			0x20, i, // local.get i
			0x6a, // i32.add
		)
	}
	body = append(body, 0x0b) // end

	compiled, err := NewRuntimeConfig().Compile(returningImportModule(sig, body))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer compiled.Close()

	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.f": HostFunc(func(_ HostModule, _ []uint64, results []uint64) {
			results[0] = I32(7)
		}),
	}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	results, err := in.Invoke("g")
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if len(results) != 1 || AsI32(results[0]) != 108 {
		t.Fatalf("g = %v; want i32(108)", results)
	}
}

func TestARM64AsyncHostCallPreservesScratchPinnedLocals(t *testing.T) {
	importSig := wasmtest.FuncType(nil, nil)
	localSig := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	body := []byte{
		0x01, 0x08, 0x7f, // one local run: eight i32 locals
	}
	for i := byte(0); i < 8; i++ {
		body = append(body,
			0x41, 10+i, // i32.const 10+i
			0x21, i, // local.set i
		)
	}
	body = append(body,
		0x10, 0x00, // call env.f (void async log path)
		0x20, 0x00, // local.get 0
	)
	for i := byte(1); i < 8; i++ {
		body = append(body,
			0x20, i, // local.get i
			0x6a, // i32.add
		)
	}
	body = append(body, 0x0b) // end

	compiled, err := NewRuntimeConfig().Compile(arm64HostCallModule(importSig, localSig, body))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer compiled.Close()

	calls := 0
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.f": HostFunc(func(_ HostModule, _, _ []uint64) { calls++ }),
	}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	results, err := in.Invoke("g")
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if calls != 1 {
		t.Fatalf("host calls = %d; want 1", calls)
	}
	if len(results) != 1 || AsI32(results[0]) != 108 {
		t.Fatalf("g = %v; want i32(108)", results)
	}
}

func TestARM64AsyncHostCallPreservesScratchPinnedGlobal(t *testing.T) {
	const nGlobals = 6
	globals := make([][]byte, nGlobals)
	for i := byte(0); i < nGlobals; i++ {
		globals[i] = wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 10 + i, 0x0b})
	}

	// All six globals receive identical loop-weighted and post-call scores. Stable
	// index ordering assigns g0-g4 to X19-X23 and g5 to X9, the first host-log
	// scratch register in the extended shared local/global pin bank.
	body := []byte{0x00, 0x03, 0x40} // no locals; loop void
	for i := byte(0); i < nGlobals; i++ {
		body = append(body, 0x23, i, 0x1a) // global.get i; drop
	}
	body = append(body,
		0x0b,       // end loop
		0x10, 0x00, // call env.f (void async log path)
	)
	for i := byte(0); i < nGlobals-1; i++ {
		body = append(body, 0x23, i, 0x1a) // keep post-call scores equal
	}
	body = append(body, 0x23, nGlobals-1, 0x0b) // global.get 5; end

	importSig := wasmtest.FuncType(nil, nil)
	localSig := wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})
	globalSection := wasmtest.Section(6, wasmtest.Vec(globals...))
	compiled, err := NewRuntimeConfig().Compile(arm64HostCallModule(importSig, localSig, body, globalSection))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer compiled.Close()

	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.f": HostFunc(func(_ HostModule, _, _ []uint64) {}),
	}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	results, err := in.Invoke("g")
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if len(results) != 1 || AsI32(results[0]) != 15 {
		t.Fatalf("g = %v; want preserved g5 value i32(15)", results)
	}
}
