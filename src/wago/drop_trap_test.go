package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

func droppedTrapModule(body []byte, memory bool) []byte {
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
	}
	if memory {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec([]byte{0x00, 0x01})))
	}
	sections = append(sections,
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	return wasmtest.Module(sections...)
}

func invokeDroppedTrap(t *testing.T, cfg *RuntimeConfig, module []byte, arg uint64) {
	t.Helper()
	compiled, err := Compile(cfg, module)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	defer compiled.Close()
	instance, err := Instantiate(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer instance.Close()
	if _, err := instance.Invoke("f", arg); err == nil {
		t.Fatal("dropped trapping expression did not trap")
	}
}

func TestDroppedDeferredExpressionsPreserveTraps(t *testing.T) {
	div := droppedTrapModule([]byte{
		0x41, 0x01, 0x20, 0x00, 0x6e, 0x1a, // 1 / local.get 0; drop
		0x41, 0x01, 0x0b,
	}, false)
	load := droppedTrapModule([]byte{
		0x20, 0x00, 0x28, 0x02, 0x00, 0x1a, // i32.load(local.get 0); drop
		0x41, 0x01, 0x0b,
	}, true)

	modes := []struct {
		name string
		cfg  *RuntimeConfig
	}{{"explicit", NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)}}
	if guardPageBuilt {
		modes = append(modes, struct {
			name string
			cfg  *RuntimeConfig
		}{"signals", NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased)})
	}
	for _, mode := range modes {
		t.Run(mode.name+"/integer-divide", func(t *testing.T) {
			invokeDroppedTrap(t, mode.cfg, div, I32(0))
		})
		t.Run(mode.name+"/memory-load", func(t *testing.T) {
			invokeDroppedTrap(t, mode.cfg, load, I32(65536))
		})
	}
}
