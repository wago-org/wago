//go:build darwin && arm64

package wago

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func watToWasm(t testing.TB, wat string) []byte {
	t.Helper()
	w2w, err := exec.LookPath("wat2wasm")
	if err != nil {
		t.Skip("wat2wasm (wabt) not on PATH")
	}
	dir := t.TempDir()
	src, out := filepath.Join(dir, "m.wat"), filepath.Join(dir, "m.wasm")
	if err := os.WriteFile(src, []byte(wat), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(w2w, src, "-o", out).CombinedOutput(); err != nil {
		t.Fatalf("wat2wasm: %v\n%s", err, output)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// passiveDataModule moved to dataseg_shared_test.go

func multiValueControlCallModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32, wasm.I64}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32, wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("pair", 0, 0), wasmtest.ExportEntry("choose", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0x12, 0x42, 0x13, 0x0b}),
			wasmtest.Code([]byte{0x20, 0x00, 0x04, 0x00, 0x10, 0x00, 0x05, 0x41, 0x07, 0x42, 0x09, 0x0b, 0x0b}),
		)),
	)
}

// signExtModule exports f(i32)->i32 = i32.extend8_s(local0).
func signExtModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0x00, 0xc0, 0x0b}))),
	)
}

func tableTestBody(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return append(out, 0x0b)
}

func tableTestI32Const(v int32) []byte {
	out := []byte{0x41}
	out = append(out, wasmtest.SLEB32(v)...)
	return out
}

func tableTestLocalGet(i uint32) []byte {
	return append([]byte{0x20}, wasmtest.ULEB(i)...)
}

func tableTestRefFunc(i uint32) []byte {
	return append([]byte{0xd2}, wasmtest.ULEB(i)...)
}

func tableTestRefNullFunc() []byte { return []byte{0xd0, 0x70} }

func tableTestImportTable(module, name string, min, max uint32) []byte {
	out := append(wasmtest.Name(module), wasmtest.Name(name)...)
	out = append(out, 0x01, 0x70)
	if max == 0 {
		out = append(out, 0x00)
		return append(out, wasmtest.ULEB(min)...)
	}
	out = append(out, 0x01)
	out = append(out, wasmtest.ULEB(min)...)
	return append(out, wasmtest.ULEB(max)...)
}

func tableTestActiveElem(offset int32, funcs ...uint32) []byte {
	out := []byte{0x00, 0x41}
	out = append(out, wasmtest.SLEB32(offset)...)
	out = append(out, 0x0b)
	return append(out, tableTestFuncIdxVec(funcs...)...)
}

func tableTestActiveElemAt(tableIdx uint32, offset int32, funcs ...uint32) []byte {
	out := []byte{0x02}
	out = append(out, wasmtest.ULEB(tableIdx)...)
	out = append(out, 0x41)
	out = append(out, wasmtest.SLEB32(offset)...)
	out = append(out, 0x0b, 0x00)
	return append(out, tableTestFuncIdxVec(funcs...)...)
}

func tableTestCallIndirect(typeIdx, tableIdx uint32) []byte {
	out := append([]byte{0x11}, wasmtest.ULEB(typeIdx)...)
	return append(out, wasmtest.ULEB(tableIdx)...)
}

func tableTestBulk(sub uint32, immediates ...uint32) []byte {
	out := append([]byte{0xfc}, wasmtest.ULEB(sub)...)
	for _, imm := range immediates {
		out = append(out, wasmtest.ULEB(imm)...)
	}
	return out
}

func tableTestFuncSection(typeIdxs ...uint32) []byte {
	items := make([][]byte, len(typeIdxs))
	for i, idx := range typeIdxs {
		items[i] = wasmtest.ULEB(idx)
	}
	return wasmtest.Section(3, wasmtest.Vec(items...))
}

func tableTestFuncIdxVec(funcs ...uint32) []byte {
	out := wasmtest.ULEB(uint32(len(funcs)))
	for _, f := range funcs {
		out = append(out, wasmtest.ULEB(f)...)
	}
	return out
}

func tableTestPassiveElem(funcs ...uint32) []byte {
	out := []byte{0x01, 0x00} // passive, elemkind funcref
	return append(out, tableTestFuncIdxVec(funcs...)...)
}

func tableTestDeclarativeElem(funcs ...uint32) []byte {
	out := []byte{0x03, 0x00} // declarative, elemkind funcref
	return append(out, tableTestFuncIdxVec(funcs...)...)
}
