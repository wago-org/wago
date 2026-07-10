//go:build linux && amd64

package wago

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func activeDataStateModule() []byte {
	initBody := tableTestBody(
		tableTestI32Const(0),
		tableTestI32Const(0),
		tableTestLocalGet(0),
		tableTestBulk(8, 0, 0),
	)
	dropBody := tableTestBody(tableTestBulk(9, 0))
	activeData := []byte{0x00, 0x41, 0x00, 0x0b}
	activeData = append(activeData, wasmtest.ULEB(1)...)
	activeData = append(activeData, 'x')

	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType(nil, nil),
		)),
		wasmtest.Section(3, append(wasmtest.ULEB(2), 0x00, 0x01)),
		wasmtest.Section(5, []byte{0x01, 0x00, 0x01}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("init", 0, 0),
			wasmtest.ExportEntry("drop", 0, 1),
			wasmtest.ExportEntry("mem", 2, 0),
		)),
		wasmtest.Section(12, wasmtest.ULEB(1)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(initBody), wasmtest.Code(dropBody))),
		wasmtest.Section(11, wasmtest.Vec(activeData)),
	)
}

func TestActiveDataSegmentStartsDroppedForBulkInstructions(t *testing.T) {
	inst, err := Instantiate(MustCompile(activeDataStateModule()), nil)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	defer inst.Close()

	if got := inst.Memory().Bytes()[0]; got != 'x' {
		t.Fatalf("active data byte = %#x, want %#x", got, byte('x'))
	}
	if _, err := inst.Invoke("init", I32(0)); err != nil {
		t.Fatalf("zero-length memory.init from active segment: %v", err)
	}
	if _, err := inst.Invoke("init", I32(1)); err == nil {
		t.Fatal("nonzero memory.init from active segment succeeded; want trap")
	}
	if _, err := inst.Invoke("drop"); err != nil {
		t.Fatalf("first data.drop active segment: %v", err)
	}
	if _, err := inst.Invoke("drop"); err != nil {
		t.Fatalf("second data.drop active segment: %v", err)
	}
}

func activeAndDeclarativeElementStateModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
			wasmtest.FuncType(nil, nil),
		)),
		tableTestFuncSection(0, 1, 2, 3, 2, 3),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("callAt", 0, 1),
			wasmtest.ExportEntry("initActive", 0, 2),
			wasmtest.ExportEntry("dropActive", 0, 3),
			wasmtest.ExportEntry("initDeclarative", 0, 4),
			wasmtest.ExportEntry("dropDeclarative", 0, 5),
		)),
		wasmtest.Section(9, wasmtest.Vec(
			tableTestActiveElem(0, 0),
			tableTestDeclarativeElem(0),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(tableTestBody(tableTestI32Const(7))),
			wasmtest.Code(tableTestBody(tableTestLocalGet(0), tableTestCallIndirect(0, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestI32Const(0), tableTestLocalGet(0), tableTestBulk(12, 0, 0))),
			wasmtest.Code(tableTestBody(tableTestBulk(13, 0))),
			wasmtest.Code(tableTestBody(tableTestI32Const(0), tableTestI32Const(0), tableTestLocalGet(0), tableTestBulk(12, 1, 0))),
			wasmtest.Code(tableTestBody(tableTestBulk(13, 1))),
		)),
	)
}

func TestActiveAndDeclarativeElementSegmentsStartDropped(t *testing.T) {
	inst := tableTestInstantiate(t, activeAndDeclarativeElementStateModule())
	defer inst.Close()

	if got := tableTestCallI32(t, inst, "callAt", I32(0)); got != 7 {
		t.Fatalf("active element was not applied: callAt(0) = %d, want 7", got)
	}
	for _, name := range []string{"initActive", "initDeclarative"} {
		if _, err := inst.Invoke(name, I32(0)); err != nil {
			t.Fatalf("%s zero length: %v", name, err)
		}
		_, err := inst.Invoke(name, I32(1))
		tableTestExpectTrap(t, err, TrapLinMemOutOfBounds)
	}
	for _, name := range []string{"dropActive", "dropDeclarative"} {
		if _, err := inst.Invoke(name); err != nil {
			t.Fatalf("%s first call: %v", name, err)
		}
		if _, err := inst.Invoke(name); err != nil {
			t.Fatalf("%s second call: %v", name, err)
		}
	}
}
