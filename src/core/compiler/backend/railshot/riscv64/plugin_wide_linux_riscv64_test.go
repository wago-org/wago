//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/plugins"
	goruntime "runtime"
	"testing"
)

func TestPluginRV64WideNativeWarmAllocations(t *testing.T) {
	previous := goruntime.GOMAXPROCS(1)
	defer goruntime.GOMAXPROCS(previous)
	e, j, args, results, trap, _ := thunkFixtureRV64(t, 4)
	for _, size := range []int32{32, 64, 128, 256, 512} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			custom, _ := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "wide", Size: size, Carrier: plugins.WasmI32})
			m := widePluginTestModule(wasm.I32, []byte{0x10, 0, 0x10, 0, 0x10, 0, 0x10, 1, 0x10, 1, 0x10, 1, 0x0b})
			types := make([]*wasm.CompType, len(m.Types))
			for i := range types {
				types[i] = &m.Types[i].SubTypes[0].Comp
			}
			functions := make([]*wasm.CompType, 3)
			for i := range functions {
				functions[i], _ = m.FuncSignature(uint32(i))
			}
			code, info, _, err := compileSWARPairsWithImports(&m.Code[0], functions[2], nil, types, functions, nil, 0, nil, nil, 0, 2, emissionMode{production: true, interruptible: true, customInstructions: pluginInstructions(widePluginTestInstructions(custom))})
			if err != nil {
				t.Fatal(err)
			}
			entry := thunkImageRV64(t, code)
			call := func() {
				if err := e.CallPrepared(entry, args, j.LinMemBase(), trap, results); err != nil {
					panic(err)
				}
			}
			allocations := testing.AllocsPerRun(64, call)
			t.Logf("RV64 emulation: size=%d frame-bytes=%d framed-function-code-bytes=%d native-entry-warm-allocations=%g", size, info.FrameBytes, len(code), allocations)
			if allocations != 0 {
				t.Fatal("native custom value call allocates", allocations)
			}
		})
	}
}
