//go:build amd64 && (linux || darwin || windows) && !tinygo

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

type divRemPairFixture struct {
	Wide, Signed, RemFirst, DropQuotient, RemOnly bool
	Gap                                           string
	Loop                                          string
}

// Module builds a controlled pair; gap variants are rejection controls. Loop
// modes are throughput (independent pairs), latency (loop-carried dividend),
// latency-biased (nonzero loop-carried dividend), and pressure (seven eager
// reinterpret carriers live across each pair).
func (f divRemPairFixture) Module() []byte {
	t, div, add, constant, reinterpret := wasm.I32, byte(0x6e), byte(0x6a), byte(0x41), []byte{0xbe, 0xbc}
	if f.Wide {
		t, div, add, constant, reinterpret = wasm.I64, 0x80, 0x7c, 0x42, []byte{0xbf, 0xbd}
	}
	if f.Signed {
		div--
	}
	rem := div + 2
	params := []wasm.ValType{t, t, t}
	results := []wasm.ValType{t, t}
	body := []byte{0}
	if f.Loop != "" {
		params[2] = wasm.I32
		results = []wasm.ValType{t}
		body = []byte{2, 1, 0x7f, 1, wasm.MustEncodeValType(t), 0x02, 0x40, 0x03, 0x40, 0x20, 3, 0x20, 2, 0x4f, 0x0d, 1}
	}
	if f.Loop == "pressure" {
		for i := 0; i < 7; i++ {
			body = append(body, 0x20, 0)
			body = append(body, reinterpret...)
		}
	}
	first, second := div, rem
	if f.RemFirst {
		first, second = rem, div
	}
	body = append(body, 0x20, 0, 0x20, 1, first)
	if f.DropQuotient {
		body = append(body, 0x1a)
		results = []wasm.ValType{t}
	}
	if f.RemOnly {
		body = []byte{0, 0x20, 0, 0x20, 1, rem}
		results = []wasm.ValType{t}
	} else {
		switch f.Gap {
		case "mutate":
			body = append(body, 0x20, 0, constant, 1, add, 0x21, 0)
		case "effect":
			body = append(body, 0x23, 0, 0x41, 1, 0x6a, 0x24, 0)
		case "callback":
			body = append(body, 0x10, 0)
		case "trap":
			body = append(body, constant, 1, constant, 0, div, 0x1a)
		case "boundary":
			body = append(body, 0x02, 0x40, 0x0b)
		case "window":
			for i := 0; i < 12; i++ {
				body = append(body, 0x01)
			}
		}
		index := byte(0)
		if f.Gap == "distinct" {
			index = 2
		}
		body = append(body, 0x20, index, 0x20, 1, second)
	}
	if f.Loop != "" {
		body = append(body, add)
		if f.Loop == "pressure" {
			for i := 0; i < 7; i++ {
				body = append(body, add)
			}
		}
		if f.Loop == "latency-biased" {
			body = append(body, constant)
			body = append(body, wasmtest.SLEB32(1048576)...)
			body = append(body, add)
		}
		if f.Loop == "latency" || f.Loop == "latency-biased" {
			body = append(body, 0x22, 0)
		}
		body = append(body, 0x20, 4, add, 0x21, 4)
		if f.Loop != "latency" && f.Loop != "latency-biased" {
			body = append(body, 0x20, 0, constant, 1, add, 0x21, 0)
		}
		body = append(body, 0x20, 3, 0x41, 1, 0x6a, 0x21, 3, 0x0c, 0, 0x0b, 0x0b, 0x20, 4)
	}
	body = append(body, 0x0b)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results), wasmtest.FuncType(nil, nil)))}
	index := uint32(0)
	if f.Gap == "callback" {
		index = 1
		imp := append(wasmtest.Name("env"), wasmtest.Name("observe")...)
		imp = append(imp, 0, 1)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(imp)))
	}
	sections = append(sections, wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))), wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, index), wasmtest.ExportEntry("observed", 3, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
	return wasmtest.Module(sections...)
}
