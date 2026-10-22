//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"runtime"
)

//go:noinline
func inlineWagoGrow(depth int) int {
	var buffer [2048]byte
	buffer[0] = byte(depth)
	if depth == 0 {
		runtime.GC()
		return 0
	}
	value := inlineWagoGrow(depth-1) + int(buffer[0])
	runtime.KeepAlive(&buffer)
	return value
}
func hostYieldLoopModule() []byte {
	body := []byte{
		1, 1, 0x7f,
		0x02, 0x40, 0x03, 0x40,
		0x20, 0, 0x45, 0x0d, 1,
		0x20, 2, 0x10, 0, 0x21, 2,
		0x20, 0, 0x41, 1, 0x6b, 0x21, 0,
		0x0c, 0, 0x0b, 0x0b, 0x20, 2, 0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "step", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}
func goHostSegmentConfig() *RuntimeConfig {
	cfg := NewRuntimeConfig()
	if runtime.GOARCH == "arm64" {
		cfg = cfg.WithOptimization("native-leaf-host", false)
	}
	return cfg
}
