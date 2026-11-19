//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

func iteratorCarrierPayloadFixture(count, typeShift, functionShift uint32, mode string) ([]byte, uint32) {
	T := func(n uint32) uint32 { return n + typeShift }
	F := func(n uint32) uint32 { return n + functionShift }
	field := func(n uint32) []byte { return iteratorFixtureSeq(iteratorFixtureRefBytes(T(n)), []byte{0}) }
	types := make([][]byte, typeShift)
	for i := range types {
		types[i] = iteratorFixtureFunc(nil, nil)
	}
	types = append(types, []byte{0x5f, 1, 0x7f, 0}, []byte{0x5e, 0x6d, 1}, iteratorFixtureSeq([]byte{0x5f}, wasmtest.Vec(field(1), []byte{0x7f, 0})), iteratorFixtureSeq([]byte{0x5f}, wasmtest.Vec(field(2), []byte{0x7f, 1})), []byte{0x50, 0, 0x5f, 1, 0x7f, 0}, iteratorFixtureSeq([]byte{0x4f, 1}, wasmtest.ULEB(T(4)), []byte{0x5f, 2, 0x7f, 0, 0x6d, 0}), iteratorFixtureFunc([]wasm.ValType{iteratorFixtureRef(T(2)), wasm.I32}, []wasm.ValType{wasm.EqRef}), iteratorFixtureFunc([]wasm.ValType{iteratorFixtureRef(T(3))}, []wasm.ValType{wasm.I32}), iteratorFixtureFunc([]wasm.ValType{iteratorFixtureRef(T(3))}, []wasm.ValType{wasm.EqRef}), iteratorFixtureFunc([]wasm.ValType{iteratorFixtureRef(T(3))}, []wasm.ValType{iteratorFixtureRef(T(4))}), iteratorFixtureFunc([]wasm.ValType{iteratorFixtureRef(T(3))}, []wasm.ValType{iteratorFixtureRef(T(4))}), iteratorFixtureFunc(nil, []wasm.ValType{wasm.I32}), iteratorFixtureFunc(nil, nil))
	if mode == "graph" {
		types[typeShift] = []byte{0x5f, 1, 0x6d, 0}
	}
	types = append(types, []byte{0x5f, 1, 0x7f, 0})

	get := iteratorFixtureSeq(iteratorFixtureLocal(1), iteratorFixtureLocal(0), iteratorFixtureCast(T(2)), iteratorFixtureFB(2, T(2), 1), []byte{0x4f, 4, 0x6d, 0, 5}, iteratorFixtureLocal(0), iteratorFixtureCast(T(2)), iteratorFixtureFB(2, T(2), 0), iteratorFixtureCast(T(1)), iteratorFixtureLocal(1), iteratorFixtureFB(11, T(1)), []byte{0x0b, 0x0b})
	has := iteratorFixtureSeq(iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureFB(2, T(3), 1), iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureFB(2, T(3), 0), iteratorFixtureCast(T(2)), iteratorFixtureFB(2, T(2), 1), []byte{0x49, 0x0b})
	payload := iteratorFixtureSeq(iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureFB(2, T(3), 0), iteratorFixtureCast(T(2)), iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureFB(2, T(3), 1), iteratorFixtureCall(F(0)), iteratorFixtureSet(1), iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureFB(2, T(3), 1), []byte{0x41, 1, 0x6a}, iteratorFixtureFB(5, T(3), 1), iteratorFixtureLocal(1), []byte{0x0b})
	clear := iteratorFixtureSeq(iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureFB(2, T(3), 0), iteratorFixtureCast(T(2)), iteratorFixtureFB(2, T(2), 0), iteratorFixtureCast(T(1)), iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureFB(2, T(3), 1), []byte{0xd0, 0x6d}, iteratorFixtureFB(14, T(1)))
	needle := iteratorFixtureSeq(iteratorFixtureCall(F(0)), iteratorFixtureSet(1))
	payload = bytes.Replace(payload, needle, iteratorFixtureSeq(needle, clear), 1)

	producer := iteratorFixtureSeq(iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureCall(F(1)), []byte{4, 0x6d}, iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureCall(F(2)), iteratorFixtureSet(1), []byte{0x41, 1}, iteratorFixtureLocal(1), iteratorFixtureFB(0, T(5)), []byte{5, 0x41, 0}, iteratorFixtureFB(0, T(4)), []byte{0x0b}, iteratorFixtureCast(T(4)), []byte{0x0b})
	wrapper := iteratorFixtureSeq(iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureCall(F(3)), iteratorFixtureCast(T(4)), []byte{0x0b})
	step := iteratorFixtureSeq(iteratorFixtureLocal(0), iteratorFixtureCast(T(3)), iteratorFixtureCall(F(4)), iteratorFixtureSet(1))
	tag := iteratorFixtureSeq(iteratorFixtureLocal(1), iteratorFixtureCast(T(4)), iteratorFixtureFB(2, T(4), 0))
	run := iteratorFixtureSeq([]byte{0x23, 3}, iteratorFixtureSet(0), step, []byte{2, 0x40, 3, 0x40, 2, 0x40}, tag, []byte{0x41, 1, 0x46, 4, 0x40}, iteratorFixtureLocal(1), iteratorFixtureCast(T(5)), iteratorFixtureFB(2, T(5), 1), iteratorFixtureSet(2), iteratorFixtureLocal(2), []byte{0xfb, 22, 0x6c, 0xfb, 30}, iteratorFixtureLocal(3), []byte{0x6a}, iteratorFixtureSet(3), step, []byte{0x0c, 2, 0x0c, 1, 0x0b}, tag, []byte{0x41, 0, 0x46, 4, 0x40, 0x0c, 3, 0x0c, 1, 0x0b, 0, 0x0b, 0, 0x0b, 0, 0x0b}, iteratorFixtureLocal(3), []byte{0x0b})
	alloc := iteratorFixtureSeq([]byte{0x41, 0}, iteratorFixtureFB(0, T(13)), []byte{0x1a})
	unbox := []byte{0xfb, 22, 0x6c, 0xfb, 30}
	if mode == "null" {
		unbox = []byte{0xd1}
	}
	if mode == "graph" {
		unbox = iteratorFixtureSeq(iteratorFixtureCast(T(0)), iteratorFixtureFB(2, T(0), 0), iteratorFixtureCast(T(13)), iteratorFixtureFB(2, T(13), 0))
	}
	run = bytes.Replace(run, []byte{0xfb, 22, 0x6c, 0xfb, 30}, unbox, 1)
	run = bytes.Replace(run, iteratorFixtureSeq(iteratorFixtureSet(2), iteratorFixtureLocal(2)), iteratorFixtureSeq(iteratorFixtureSet(2), alloc, iteratorFixtureLocal(2)), 1)

	index := iteratorFixtureSeq([]byte{0x23, 3}, iteratorFixtureCast(T(3)), iteratorFixtureFB(2, T(3), 1), []byte{0x0b})

	var drained []byte
	for i := uint32(0); i < count; i++ {
		drained = append(drained, iteratorFixtureSeq([]byte{0x23, 1}, iteratorFixtureConst(i), iteratorFixtureFB(11, T(1)), []byte{0xd1, 0x45, 4, 0x40, 0, 0xb})...)
	}
	index = iteratorFixtureSeq(drained, index)
	declarations := make([][]byte, functionShift)
	bodies := make([][]byte, functionShift)
	for i := range declarations {
		declarations[i] = wasmtest.ULEB(T(12))
		bodies[i] = wasmtest.Code([]byte{0x0b})
	}
	for _, n := range []uint32{6, 7, 8, 9, 10, 11, 11} {
		declarations = append(declarations, wasmtest.ULEB(T(n)))
	}
	bodies = append(bodies, wasmtest.Code(get), wasmtest.Code(has), iteratorFixtureBody([]byte{1, 1, 0x6d}, payload), iteratorFixtureBody([]byte{1, 1, 0x6d}, producer), wasmtest.Code(wrapper), iteratorFixtureBody(iteratorFixtureSeq([]byte{4, 1, 0x63}, wasmtest.ULEB(T(3)), []byte{1, 0x6d, 1, 0x6d, 1, 0x7f}), run), wasmtest.Code(index))
	var elements []byte
	for i := uint32(0); i < count; i++ {
		switch mode {
		case "null":
			elements = append(elements, 0xd0, 0x6d)
		case "graph":
			elements = append(elements, iteratorFixtureSeq(iteratorFixtureConst(42+i), iteratorFixtureFB(0, T(13)), iteratorFixtureFB(0, T(0)))...)
		default:
			elements = append(elements, iteratorFixtureConst(i)...)
			elements = append(elements, 0xfb, 28)
		}
	}
	initial := []byte{0x41, 42}
	if mode == "graph" {
		initial = []byte{0xd0, 0x6d}
	}

	globals := wasmtest.Vec(iteratorFixtureGlobal(T(0), iteratorFixtureSeq(initial, iteratorFixtureFB(0, T(0)))), iteratorFixtureGlobal(T(1), iteratorFixtureSeq(elements, iteratorFixtureFB(8, T(1), count))), iteratorFixtureGlobal(T(2), iteratorFixtureSeq([]byte{0x23, 1}, iteratorFixtureConst(count), iteratorFixtureFB(0, T(2)))), iteratorFixtureGlobal(T(3), iteratorFixtureSeq([]byte{0x23, 2, 0x41, 0}, iteratorFixtureFB(0, T(3)))))
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(declarations...)), wasmtest.Section(6, globals), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, F(5)), wasmtest.ExportEntry("index", 0, F(6)), wasmtest.ExportEntry("next", 0, F(4)))), wasmtest.Section(10, wasmtest.Vec(bodies...))), T(5)
}

func gcOptionTestSwitch(t *testing.T, enabled string) {
	t.Helper()
	disabled := "1"
	if enabled == "1" {
		disabled = "0"
	}
	t.Setenv("WAGO_AMD64_NO_GC_CARRIER_ELISION", disabled)
}
