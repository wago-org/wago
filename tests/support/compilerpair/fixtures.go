// Package compilerpair contains bounded fixtures for test-only compiler-path comparisons.
package compilerpair

import (
	"bytes"
	"fmt"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type Fixture struct {
	Wide          []uint64 // Bit j marks a 64-bit result slot in function i. Unset slots are i32.
	Events        [][]uint32
	ID            string
	Wasm, Invalid []byte
	Shared        []bool
	Want          [][]uint64
	Trap          []bool
	Memory        []byte
	Reason        string // Source premise, not a production fallback diagnostic.
}

type function struct {
	params, results []wasm.ValType
	body            []byte
}

func module(functions []function, extraTypes [][]byte, memory bool) []byte {
	var types, signatures, bodies, exports [][]byte
	for i, f := range functions {
		types = append(types, wasmtest.FuncType(f.params, f.results))
		signatures = append(signatures, wasmtest.ULEB(uint32(i)))
		bodies = append(bodies, append(wasmtest.ULEB(uint32(len(f.body))), f.body...))
		exports = append(exports, wasmtest.ExportEntry(fmt.Sprintf("f%d", i), 0, uint32(i)))
	}
	types = append(types, extraTypes...)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, wasmtest.Vec(signatures...))}
	if memory {
		sections = append(sections, wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 1})))
		exports = append(exports, wasmtest.ExportEntry("memory", 2, 0))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(bodies...)))
	return wasmtest.Module(sections...)
}

// Fixtures fixes all source bytes and expected values. The boundary cases sit
// at the current 256-local, 32-control and 16,384 admission-budget limits.
func Fixtures() []Fixture {
	i32 := []wasm.ValType{wasm.I32}
	one := func(id string, body []byte, shared bool, want uint64, reason string) Fixture {
		return Fixture{ID: id, Wasm: module([]function{{results: i32, body: body}}, nil, false), Shared: []bool{shared}, Want: [][]uint64{{want}}, Trap: []bool{false}, Reason: reason}
	}
	out := []Fixture{one("small", []byte{0, 0x41, 42, 0x0b}, true, 42, "admitted integer constant")}

	wideBody := append([]byte{0, 0x42}, wasmtest.SLEB64(9007199254740993)...)
	wideBody = append(wideBody, 0x0b)
	out = append(out, Fixture{ID: "wide-leaf", Wasm: module([]function{{results: []wasm.ValType{wasm.I64}, body: wideBody}}, nil, false), Shared: []bool{true}, Want: [][]uint64{{9007199254740993}}, Wide: []uint64{1}, Trap: []bool{false}, Reason: "exact i64 value above binary64 integer precision"})
	out = append(out, Fixture{ID: "empty", Wasm: module([]function{{body: []byte{0, 0x0b}}}, nil, false), Shared: []bool{false}, Want: [][]uint64{nil}, Trap: []bool{false}, Reason: "void result"})
	for _, n := range []int{256, 257} {
		body := append([]byte{1}, wasmtest.ULEB(uint32(n))...)
		body = append(body, 0x7f, 0x20)
		body = append(body, wasmtest.ULEB(uint32(n-1))...)
		body = append(body, 0x0b)
		out = append(out, one(fmt.Sprintf("locals-%d", n), body, n == 256, 0, "local-count admission boundary"))
	}
	for _, n := range []int{32, 33} {
		body := []byte{0}
		for range n {
			body = append(body, 0x02, 0x7f)
		}
		body = append(body, 0x41, 42)
		body = append(body, bytes.Repeat([]byte{0x0b}, n+1)...)
		out = append(out, one(fmt.Sprintf("controls-%d", n), body, n == 32, 42, "control-depth admission boundary"))
	}
	for _, n := range []int{16380, 16381} {
		body := append([]byte{0}, bytes.Repeat([]byte{1}, n)...)
		body = append(body, 0x41, 42, 0x0b)
		out = append(out, one(fmt.Sprintf("budget-%d", n+4), body, n == 16380, 42, "instruction admission-budget boundary"))
	}
	// Each invalid variant changes one premise inside a typed region. The outer
	// result is discarded, so it does not introduce a second return-type fault.
	typed := func(loop bool, invalid bool) []byte {
		constant := byte(0x41)
		if invalid {
			constant = 0x42
		}
		op := byte(0x02)
		if loop {
			op = 0x03
		}
		body := []byte{0, constant, 41, op, 1, 0x41, 1, 0x6a, 0x0b, 0x0b}
		return module([]function{{results: i32, body: body}}, [][]byte{wasmtest.FuncType(i32, i32)}, false)
	}
	for _, loop := range []bool{false, true} {
		id := "typed-block"
		if loop {
			id = "typed-loop"
		}
		out = append(out, Fixture{ID: id, Wasm: typed(loop, false), Invalid: typed(loop, true), Shared: []bool{false}, Want: [][]uint64{{42}}, Trap: []bool{false}, Reason: "indexed block signature/loop; invalid variant changes incoming parameter width"})
	}
	branches := func(invalid bool) []byte {
		outer, constant := byte(0x7f), byte(0x41)
		if invalid {
			outer, constant = 0x7e, 0x42
		}
		body := []byte{0, 0x02, outer, 0x02, 0x7f, 0x41, 7, 0x41, 0, 0x0e, 1, 0, 1, 0x0b, 0x1a, constant, 7, 0x0b, 0x1a, 0x41, 42, 0x0b}
		return module([]function{{results: i32, body: body}}, nil, false)
	}
	out = append(out, Fixture{ID: "typed-br-table", Wasm: branches(false), Invalid: branches(true), Shared: []bool{false}, Want: [][]uint64{{42}}, Trap: []bool{false}, Reason: "arbitrary branch; invalid variant gives its labels incompatible result widths"})
	unreachable := func(invalid bool) []byte {
		constant := byte(0x41)
		if invalid {
			constant = 0x42
		}
		body := []byte{0, 0x02, 0x7f, 0, 0x02, 1, 0x1a, constant, 7, 0x0b, 0x0b, 0x0b}
		return module([]function{{results: i32, body: body}}, [][]byte{wasmtest.FuncType(i32, i32)}, false)
	}
	out = append(out, Fixture{ID: "unreachable-typed", Wasm: unreachable(false), Invalid: unreachable(true), Shared: []bool{false}, Want: [][]uint64{nil}, Trap: []bool{true}, Reason: "unreachable indexed block; invalid variant changes a concrete result width"})
	// A discarded multi-result call is visible to admission even when its values
	// are unused. Check both exports so no compiled function is merely counted.

	multiCall := func(invalid bool) []byte {
		second := wasm.I64
		if invalid {
			second = wasm.F64
		}
		return module([]function{{results: i32, body: []byte{0, 0x10, 1, 0x1a, 0x1a, 0x41, 42, 0x0b}}, {results: []wasm.ValType{wasm.I32, second}, body: []byte{0, 0x41, 7, 0x42, 9, 0x0b}}}, nil, false)
	}
	out = append(out, Fixture{ID: "discard-multi-call", Wide: []uint64{0, 2}, Wasm: multiCall(false), Invalid: multiCall(true), Shared: []bool{false, false}, Want: [][]uint64{{42}, {7, 9}}, Trap: []bool{false, false}, Reason: "call and multiple results; invalid variant changes one callee result type"})

	// Reuse the parameterized, catch-free shapes from the existing #739
	// regression. The path matrix adds admission evidence to their execution.
	for _, trapped := range []bool{false, true} {
		makeEH := func(invalid bool) []byte {
			constant := byte(0x41)
			if invalid {
				constant = 0x42
			}
			body := []byte{0, 0x41, 41}
			if trapped {
				body = []byte{0, 0}
			}
			body = append(body, 0x1f, 1, 0, 0x1a, constant, 42, 0x0b, 0x0b)
			return module([]function{{results: i32, body: body}}, [][]byte{wasmtest.FuncType(i32, i32)}, false)
		}
		id, want := "typed-try-table", []uint64{42}
		if trapped {
			id, want = "unreachable-try-table", nil
		}
		out = append(out, Fixture{ID: id, Wasm: makeEH(false), Invalid: makeEH(true), Shared: []bool{false}, Want: [][]uint64{want}, Trap: []bool{trapped}, Reason: "indexed exception region; invalid variant changes concrete result width"})
	}
	out = append(out, one("gc-i31", []byte{0, 0x41, 42, 0xfb, 0x1c, 0xfb, 0x1d, 0x0b}, false, 42, "GC reference construction and extraction; no collector allocation"))

	out = append(out, one("float-conversion", []byte{0, 0x43, 0, 0, 0x28, 0x42, 0xa8, 0x0b}, false, 42, "floating-point operation"))
	vec := []byte{0, 0xfd, 0x0c}
	vec = append(vec, bytes.Repeat([]byte{42}, 16)...)
	vec = append(vec, 0xfd, 0x16, 0, 0x0b)
	out = append(out, one("simd-extract", vec, false, 42, "SIMD operation"))
	out = append(out, Fixture{ID: "memory-effect", Wasm: module([]function{{results: i32, body: []byte{0, 0x41, 0, 0x41, 42, 0x36, 2, 0, 0x41, 0, 0x28, 2, 0, 0x0b}}}, nil, true), Shared: []bool{false}, Want: [][]uint64{{42}}, Trap: []bool{false}, Memory: []byte{42, 0, 0, 0}, Reason: "memory effect"})

	imp := append(append(wasmtest.Name("env"), wasmtest.Name("event")...), 0, 0)
	events := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(i32, nil), wasmtest.FuncType(nil, i32))),
		wasmtest.Section(2, wasmtest.Vec(imp)), wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f0", 0, 1), wasmtest.ExportEntry("f1", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 11, 0x10, 0, 0x41, 22, 0x10, 0, 0x10, 2, 0x0b}), wasmtest.Code([]byte{0x41, 42, 0x0b}))),
	)
	out = append(out, Fixture{ID: "ordered-events", Wasm: events, Shared: []bool{false, true}, Want: [][]uint64{{42}, {42}}, Trap: []bool{false, false}, Events: [][]uint32{{11, 22}, nil}, Reason: "deferred host events followed by admitted integer callee"})
	many := Fixture{ID: "many-functions", Reason: "48 admitted integer functions"}
	var functions []function
	for i := 0; i < 48; i++ {
		functions = append(functions, function{results: i32, body: []byte{0, 0x41, byte(i), 0x0b}})
		many.Shared = append(many.Shared, true)
		many.Want = append(many.Want, []uint64{uint64(i)})
		many.Trap = append(many.Trap, false)
	}
	many.Wasm = module(functions, nil, false)
	out = append(out, many)
	return out
}
