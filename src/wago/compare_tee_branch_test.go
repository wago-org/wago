//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"testing"

	corewasm "github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// A compare written by local.tee and consumed by the next br_if, if or select
// must leave the local holding the boolean on both edges. Each export is
// (func (param $x i32) (result i32) (local $ok i32) (local $n i32) (local $s i32))
// and starts with `(local.set $ok (i32.const 7))`, so a stale read of $ok is
// visible whichever way the branch goes. The compare operand comes from $x
// directly, through a direct, indirect or imported call of an identity function, or
// after an unrelated host call; a call makes the function keep local slot state,
// so branch edges converge locals to their frame slots.
func TestCompareTeeBranchKeepsLocal(t *testing.T) {
	const (
		opBlock        = 0x02
		opLoop         = 0x03
		opIf           = 0x04
		opElse         = 0x05
		opEnd          = 0x0b
		opBrIf         = 0x0d
		opCall         = 0x10
		opCallIndirect = 0x11
		opDrop         = 0x1a
		opSelect       = 0x1b
		opGet          = 0x20
		opSet          = 0x21
		opTee          = 0x22
		opI32          = 0x41
		opEqz          = 0x45
		opLtS          = 0x48
		opGtS          = 0x4a
		opAdd          = 0x6a
		blockVoid      = 0x40
		blockI32       = 0x7f
		x, ok          = 0x00, 0x01
		n, s           = 0x02, 0x03
		hostID         = 0x00 // imported env.id
		wasmID         = 0x01 // (func (param i32) (result i32) local.get 0), also table slot 0
	)
	cat := func(parts ...[]byte) []byte {
		var out []byte
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	i32 := func(v int32) []byte { return append([]byte{opI32}, wasmtest.SLEB32(v)...) }
	prelude := cat(i32(7), []byte{opSet, ok})

	sources := []struct {
		name string
		// value produces an i32 derived from v (a local index) on the stack.
		value func(v byte) []byte
	}{
		{"direct", func(v byte) []byte { return []byte{opGet, v} }},
		{"wasm call", func(v byte) []byte { return []byte{opGet, v, opCall, wasmID} }},
		{"indirect call", func(v byte) []byte { return cat([]byte{opGet, v}, i32(0), []byte{opCallIndirect, 0, 0}) }},
		{"host call", func(v byte) []byte { return []byte{opGet, v, opCall, hostID} }},
		{"after host call", func(v byte) []byte { return []byte{opGet, v, opCall, hostID, opDrop, opGet, v} }},
	}
	type variant struct {
		name string
		body func(value func(byte) []byte) []byte
		// want maps an argument to the expected result.
		want map[int32]int32
	}
	// gt is (i32.gt_s (value $x) (i32.const -1)).
	gt := func(value func(byte) []byte) []byte { return cat(value(x), i32(-1), []byte{opGtS}) }
	variants := []variant{
		{
			// (block (br_if 0 (local.tee $ok gt))) (local.get $ok)
			name: "tee br_if",
			body: func(v func(byte) []byte) []byte {
				return cat([]byte{opBlock, blockVoid}, gt(v), []byte{opTee, ok, opBrIf, 0, opEnd, opGet, ok})
			},
			want: map[int32]int32{5: 1, -5: 0},
		},
		{
			// (block (br_if 0 (local.tee $ok (i32.eqz gt)))) (local.get $ok)
			name: "eqz tee br_if",
			body: func(v func(byte) []byte) []byte {
				return cat([]byte{opBlock, blockVoid}, gt(v), []byte{opEqz, opTee, ok, opBrIf, 0, opEnd, opGet, ok})
			},
			want: map[int32]int32{5: 0, -5: 1},
		},
		{
			// (block (br_if 0 (i32.eqz (local.tee $ok gt)))) (local.get $ok)
			name: "tee eqz br_if",
			body: func(v func(byte) []byte) []byte {
				return cat([]byte{opBlock, blockVoid}, gt(v), []byte{opTee, ok, opEqz, opBrIf, 0, opEnd, opGet, ok})
			},
			want: map[int32]int32{5: 1, -5: 0},
		},
		{
			// (i32.add (block (result i32) (i32.add (br_if 0 (i32.const 100)
			// (local.tee $ok gt)) (i32.const 200))) (local.get $ok))
			name: "tee br_if with value",
			body: func(v func(byte) []byte) []byte {
				return cat([]byte{opBlock, blockI32}, i32(100), gt(v), []byte{opTee, ok, opBrIf, 0}, i32(200), []byte{opAdd, opEnd, opGet, ok, opAdd})
			},
			want: map[int32]int32{5: 101, -5: 300},
		},
		{
			// (block (block (br_if 1 (local.tee $ok gt))) (local.set $ok (i32.const 9))) (local.get $ok)
			name: "tee br_if outer",
			body: func(v func(byte) []byte) []byte {
				return cat([]byte{opBlock, blockVoid, opBlock, blockVoid}, gt(v), []byte{opTee, ok, opBrIf, 1, opEnd}, i32(9), []byte{opSet, ok, opEnd, opGet, ok})
			},
			want: map[int32]int32{5: 1, -5: 9},
		},
		{
			// (i32.add (select (i32.const 10) (i32.const 20) (local.tee $ok gt)) (local.get $ok))
			name: "tee select",
			body: func(v func(byte) []byte) []byte {
				return cat(i32(10), i32(20), gt(v), []byte{opTee, ok, opSelect, opGet, ok, opAdd})
			},
			want: map[int32]int32{5: 11, -5: 20},
		},
		{
			// (i32.add (if (result i32) (local.tee $ok gt) (then (i32.const 100))
			// (else (i32.const 200))) (local.get $ok))
			name: "tee if",
			body: func(v func(byte) []byte) []byte {
				return cat(gt(v), []byte{opTee, ok, opIf, blockI32}, i32(100), []byte{opElse}, i32(200), []byte{opEnd, opGet, ok, opAdd})
			},
			want: map[int32]int32{5: 101, -5: 200},
		},
		{
			// (loop (local.set $s (i32.add (local.get $s) (local.get $ok)))
			//   (br_if 0 (local.tee $ok (i32.lt_s (value (local.tee $n (i32.add (local.get $n) (i32.const 1)))) (local.get $x)))))
			// (local.get $s)
			// The loop top reads $ok on every backedge: 7 + 1*4 for $x = 5.
			name: "tee br_if loop",
			body: func(v func(byte) []byte) []byte {
				counter := cat([]byte{opGet, n}, i32(1), []byte{opAdd, opTee, n})
				// value(n) re-reads $n after the tee so every source shape applies.
				return cat([]byte{opLoop, blockVoid, opGet, s, opGet, ok, opAdd, opSet, s},
					counter, []byte{opDrop}, v(n), []byte{opGet, x, opLtS, opTee, ok, opBrIf, 0, opEnd, opGet, s})
			},
			want: map[int32]int32{5: 11, -5: 7},
		},
	}

	ii := wasmtest.FuncType([]corewasm.ValType{corewasm.I32}, []corewasm.ValType{corewasm.I32})
	imp := cat(wasmtest.Name("env"), wasmtest.Name("id"), []byte{0x00}, wasmtest.ULEB(0))
	locals := []byte{0x01, 0x03, blockI32} // three i32 locals: $ok, $n, $s
	code := func(body []byte) []byte {
		fn := cat(locals, body, []byte{opEnd})
		return cat(wasmtest.ULEB(uint32(len(fn))), fn)
	}
	for _, src := range sources {
		for _, vr := range variants {
			t.Run(src.name+"/"+vr.name, func(t *testing.T) {
				mod := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(ii)),
					wasmtest.Section(2, wasmtest.Vec(imp)),
					wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
					wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
					wasmtest.Section(9, wasmtest.Vec(cat([]byte{0x00}, i32(0), []byte{opEnd}, wasmtest.Vec(wasmtest.ULEB(wasmID))))),
					wasmtest.Section(10, wasmtest.Vec(
						wasmtest.Code([]byte{opGet, 0, opEnd}),
						code(cat(prelude, vr.body(src.value))),
					)),
				)
				imports := NewImports()
				imports.HostFunc("env", "id", CallerHostCallFunc(func(_ Caller, call HostCall) {
					call.ResultSlots()[0] = call.ParamSlots()[0]
				})).Params(ValI32).Results(ValI32)
				in := instantiateRegressionAPIModule(t, mod, imports)
				for arg, want := range vr.want {
					assertRegressionAPIResults(t, in, "run", []uint64{uint64(uint32(arg))}, []uint64{uint64(uint32(want))})
				}
			})
		}
	}
}
