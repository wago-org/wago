//go:build (linux || darwin || windows) && amd64 && !tinygo

package wago

import (
	"encoding/binary"
	"math"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func readTestMXCSR() uint32
func writeTestMXCSR(uint32)

func mxcsrHostResumeModule() []byte {
	body := []byte{0x44}
	body = binary.LittleEndian.AppendUint64(body, math.Float64bits(1))
	body = append(body, 0x44)
	body = binary.LittleEndian.AppendUint64(body, math.Float64bits(0x1p-53))
	body = append(body, 0xa0) // f64.add before the host call
	body = append(body, 0x44)
	body = binary.LittleEndian.AppendUint64(body, math.Float64bits(-1))
	body = append(body, 0x44)
	body = binary.LittleEndian.AppendUint64(body, math.Float64bits(-0x1p-53))
	body = append(body,
		0x10, 0x00, // call imported host function while the second pair is live
		0xa0, // f64.add after resuming
		0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.F64, wasm.F64}),
		)),
		wasmtest.Section(2, wasmtest.Vec(portableFuncImportEntry("env", "yield", 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func mxcsrPreparedIntModule() []byte {
	body := []byte{0x44}
	body = binary.LittleEndian.AppendUint64(body, math.Float64bits(1))
	body = append(body, 0x44)
	body = binary.LittleEndian.AppendUint64(body, math.Float64bits(0x1p-53))
	body = append(body,
		0xa0, // f64.add
		0xbd, // i64.reinterpret_f64
		0xa7, // i32.wrap_i64: low bit distinguishes nearest-even from upward
		0x0b,
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestGuestMXCSRIsolatedAcrossHostResume(t *testing.T) {
	compiled := MustCompile(mxcsrHostResumeModule())
	defer compiled.Close()
	preparedCompiled := MustCompile(mxcsrPreparedIntModule())
	defer preparedCompiled.Close()
	const (
		initialHostMXCSR = uint32(0x1f81) // canonical control, invalid status set
		resumedHostMXCSR = uint32(0x1f84) // canonical control, divide-by-zero status set
	)
	var hostMXCSR uint32
	instance, err := Instantiate(compiled, InstantiateOptions{Imports: testImports(
		"env.yield",
		slotHostFunc(func(HostModule, []uint64, []uint64) {
			hostMXCSR = readTestMXCSR()
			writeTestMXCSR(resumedHostMXCSR)
		}),
	)})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	preparedInstance, err := Instantiate(preparedCompiled)
	if err != nil {
		t.Fatal(err)
	}
	defer preparedInstance.Close()
	preparedFunc, err := preparedInstance.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	if preparedFunc.directEntry == 0 {
		t.Fatal("integer fixture did not select a direct prepared entry")
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	original := readTestMXCSR()
	writeTestMXCSR(initialHostMXCSR)
	defer writeTestMXCSR(original)

	prepared, err := preparedFunc.Invoke()
	if err != nil || len(prepared) != 1 || AsI32(prepared[0]) != 0 {
		t.Fatalf("prepared integer entry = %v, %v; want [0], nil", prepared, err)
	}
	if after := readTestMXCSR(); after != initialHostMXCSR {
		t.Fatalf("MXCSR after prepared entry = %#x; want caller state %#x", after, initialHostMXCSR)
	}
	writeTestMXCSR(initialHostMXCSR)

	got, err := instance.Invoke("run")
	if err != nil {
		t.Fatal(err)
	}
	want := []uint64{math.Float64bits(1), math.Float64bits(-1)}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("run = %#x; want nearest-even %#x", got, want)
	}
	if hostMXCSR != initialHostMXCSR {
		t.Fatalf("host callback MXCSR = %#x; want caller state %#x", hostMXCSR, initialHostMXCSR)
	}
	if after := readTestMXCSR(); after != resumedHostMXCSR {
		t.Fatalf("MXCSR after invocation = %#x; want latest host state %#x", after, resumedHostMXCSR)
	}
}
