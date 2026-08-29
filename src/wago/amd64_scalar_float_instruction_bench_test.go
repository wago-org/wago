//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

type amd64ScalarFloatBenchmarkCase struct {
	name       string
	opcode     byte
	constPool  bool
	operand    float64
	operations int
}

func amd64ScalarFloatInstructionBenchmarkModule(tc amd64ScalarFloatBenchmarkCase) []byte {
	body := []byte{
		0x01, 0x01, 0x7f, // local 3: i32 counter
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x03, 0x20, 0x00, 0x4f, 0x0d, 0x01, // if counter >= iterations, break
	}
	if tc.opcode != 0 {
		body = append(body, 0x20, 0x01)             // accumulator
		if tc.opcode >= 0xa4 && tc.opcode <= 0xa6 { // f64.min/max/copysign
			body = append(body, 0x20, 0x02)
		}
		body = append(body, tc.opcode, 0x21, 0x01) // operation; local.set accumulator
	}
	body = append(body,
		0x20, 0x03, 0x41, 0x01, 0x6a, 0x21, 0x03, // counter++
		0x0c, 0x00, // continue
		0x0b, 0x0b,
		0x20, 0x01, // return accumulator
		0x0b,
	)
	funcType := wasmtest.FuncType(
		[]wasm.ValType{wasm.I32, wasm.F64, wasm.F64},
		[]wasm.ValType{wasm.F64},
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

// BenchmarkAMD64ScalarFloatInstruction isolates selected scalar floating-point
// lowerings in dependent guest loops. One public invocation executes b.N guest
// iterations, so host-call overhead is amortized.
func BenchmarkAMD64ScalarFloatInstruction(b *testing.B) {
	cases := []amd64ScalarFloatBenchmarkCase{
		{name: "loop-control", constPool: true},
		{name: "f64.abs/gpr-mask", opcode: 0x99, operations: 1},
		{name: "f64.abs/const-pool", opcode: 0x99, constPool: true, operations: 1},
		{name: "f64.neg/gpr-mask", opcode: 0x9a, operations: 1},
		{name: "f64.neg/const-pool", opcode: 0x9a, constPool: true, operations: 1},
		{name: "f64.ceil", opcode: 0x9b, constPool: true, operations: 1},
		{name: "f64.floor", opcode: 0x9c, constPool: true, operations: 1},
		{name: "f64.trunc", opcode: 0x9d, constPool: true, operations: 1},
		{name: "f64.nearest", opcode: 0x9e, constPool: true, operations: 1},
		{name: "f64.sqrt", opcode: 0x9f, constPool: true, operations: 1},
		{name: "f64.min/distinct", opcode: 0xa4, constPool: true, operand: 1000, operations: 1},
		{name: "f64.min/equal-steady", opcode: 0xa4, constPool: true, operand: -1, operations: 1},
		{name: "f64.max/distinct", opcode: 0xa5, constPool: true, operand: -1, operations: 1},
		{name: "f64.max/equal-steady", opcode: 0xa5, constPool: true, operand: 1000, operations: 1},
		{name: "f64.copysign/gpr-mask", opcode: 0xa6, operand: -1, operations: 1},
		{name: "f64.copysign/const-pool", opcode: 0xa6, constPool: true, operand: -1, operations: 1},
	}
	const seed = 123.75
	seedBits := math.Float64bits(seed)
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			cfg := NewRuntimeConfig().WithOptimization("v128-const-cache", tc.constPool)
			compiled, err := Compile(cfg, amd64ScalarFloatInstructionBenchmarkModule(tc))
			if err != nil {
				b.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer instance.Close()

			operandBits := math.Float64bits(tc.operand)
			got, err := instance.Invoke("run", 1, seedBits, operandBits)
			if err != nil || len(got) != 1 {
				b.Fatalf("warm run = %v, %v", got, err)
			}
			want := seed
			switch tc.opcode {
			case 0x99:
				want = math.Abs(seed)
			case 0x9a:
				want = -seed
			case 0x9b:
				want = math.Ceil(seed)
			case 0x9c:
				want = math.Floor(seed)
			case 0x9d:
				want = math.Trunc(seed)
			case 0x9e:
				want = math.RoundToEven(seed)
			case 0x9f:
				want = math.Sqrt(seed)
			case 0xa4:
				want = math.Min(seed, tc.operand)
			case 0xa5:
				want = math.Max(seed, tc.operand)
			case 0xa6:
				want = math.Copysign(seed, tc.operand)
			}
			if got[0] != math.Float64bits(want) {
				b.Fatalf("warm run = %#x, want %#x", got[0], math.Float64bits(want))
			}
			if uint64(b.N) > uint64(^uint32(0)) {
				b.Fatalf("iteration count %d exceeds the i32 guest-loop limit", b.N)
			}
			b.ReportAllocs()
			b.ResetTimer()
			got, err = instance.Invoke("run", uint64(uint32(b.N)), seedBits, operandBits)
			b.StopTimer()
			if err != nil || len(got) != 1 {
				b.Fatalf("run(%d) = %v, %v", b.N, got, err)
			}
			benchUintSink = got[0]
			if tc.operations != 0 {
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*tc.operations), "ns/instruction")
			}
		})
	}
}
