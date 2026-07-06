// Package exttest builds tiny wasm modules for testing built-in extensions: a
// guest that imports one host function and exports a no-argument "run" whose
// body calls it. It depends only on the low-level wasm-encoding test helpers, so
// extension packages can be tested through the public wago API alone.
package exttest

import "github.com/wago-org/wago/testutil/wasmtest"

// WebAssembly value-type bytes, for describing import/run signatures without
// importing the internal wasm package.
const (
	I32 byte = 0x7f
	I64 byte = 0x7e
	F32 byte = 0x7d
	F64 byte = 0x7c
)

// Module builds a module that imports (impMod.impName) with signature
// impParams->impResults, and exports "run" with signature ()->runResults whose
// body is runBody (raw instruction bytes, ending in 0x0b). runBody is expected to
// push any import arguments as constants and call function 0 (the import). If mem
// is non-nil the module declares and exports a 1-page "memory" holding mem as
// active data at offset 0.
func Module(impMod, impName string, impParams, impResults, runResults, runBody, mem []byte) []byte {
	ft0 := functype(impParams, impResults)                                              // import signature (type 0)
	ft1 := functype(nil, runResults)                                                    // run signature (type 1)
	imp := append(append(wasmtest.Name(impMod), wasmtest.Name(impName)...), 0x00, 0x00) // func, type 0

	secs := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(ft0, ft1)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))), // run: one local func, type 1
	}
	exports := [][]byte{wasmtest.ExportEntry("run", 0, 1)} // func index 1
	if mem != nil {
		memType := append([]byte{0x00}, wasmtest.ULEB(1)...) // flags 0 (no max), min 1 page
		secs = append(secs, wasmtest.Section(5, wasmtest.Vec(memType)))
		exports = append(exports, wasmtest.ExportEntry("memory", 2, 0))
	}
	secs = append(secs, wasmtest.Section(7, wasmtest.Vec(exports...)))
	secs = append(secs, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(runBody))))
	if mem != nil {
		// active data segment: flags 0, offset i32.const 0, then the bytes.
		data := append([]byte{0x00, 0x41, 0x00, 0x0b}, append(wasmtest.ULEB(uint32(len(mem))), mem...)...)
		secs = append(secs, wasmtest.Section(11, wasmtest.Vec(data)))
	}
	return wasmtest.Module(secs...)
}

func functype(params, results []byte) []byte {
	out := []byte{0x60}
	out = append(out, wasmtest.ULEB(uint32(len(params)))...)
	out = append(out, params...)
	out = append(out, wasmtest.ULEB(uint32(len(results)))...)
	out = append(out, results...)
	return out
}
