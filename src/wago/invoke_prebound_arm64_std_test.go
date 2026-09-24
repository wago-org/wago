//go:build arm64 && !tinygo && (linux || darwin || windows)

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestInvokeCachedPreboundIntAfterCacheEviction(t *testing.T) {
	const exports = 5 // one more than Instance's invoke-cache capacity
	funcs := make([][]byte, exports)
	names := make([][]byte, exports)
	codes := make([][]byte, exports)
	for i := 0; i < exports; i++ {
		funcs[i] = wasmtest.ULEB(0)
		names[i] = wasmtest.ExportEntry(fmt.Sprintf("f%d", i), 0, uint32(i))
		codes[i] = wasmtest.Code([]byte{0x41, byte(i + 1), 0x0b})
	}
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(funcs...)),
		wasmtest.Section(7, wasmtest.Vec(names...)),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
	compiled := MustCompile(module)
	defer compiled.Close()
	in, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for pass := 0; pass < 3; pass++ {
		for i := 0; i < exports; i++ {
			name := fmt.Sprintf("f%d", i)
			out, err := in.Invoke(name)
			if err != nil || len(out) != 1 || out[0] != uint64(i+1) {
				t.Fatalf("pass %d %s: result %v, error %v", pass, name, out, err)
			}
			if ic := in.findInvokeCache(name); ic == nil || !ic.directIntFast || !ic.directIntBounded {
				t.Fatalf("pass %d %s: bounded direct cache not selected", pass, name)
			}
		}
	}
}
