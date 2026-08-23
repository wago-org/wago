//go:build arm64

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

// TestARM64HostCallPreservesExtendedPinnedLocals keeps eight integer locals hot
// across a returning host call. The arm64 pin pool assigns the first five to
// X19-X23 and may use X9-X11 for the next three. Host-call setup also uses
// X9-X11 as fixed scratch, so those locals must be homed before scratch setup.
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
