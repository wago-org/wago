//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"math/bits"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/wasmtest"
)

const (
	amd64DivBenchmarkSeed = uint64(0xfedcba9876543210)
	amd64DivBenchmarkAdd  = uint64(0x123456789)
)

type amd64DivBenchmarkCase struct {
	name       string
	opcode     byte
	constant   bool
	divisor    uint64
	magic      bool
	operations uint64
}

func amd64DivInstructionBenchmarkModule(tc amd64DivBenchmarkCase) []byte {
	body := []byte{
		0x01, 0x01, 0x7f, // local 3: i32 counter
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x03, 0x20, 0x00, 0x4f, 0x0d, 0x01, // if counter >= iterations, break
		0x20, 0x01, // accumulator
	}
	if tc.opcode != 0 {
		if tc.constant {
			body = append(body, 0x42)
			body = append(body, wasmtest.SLEB64(int64(tc.divisor))...)
		} else {
			body = append(body, 0x20, 0x02) // dynamic divisor
		}
		body = append(body, tc.opcode)
	}
	body = append(body, 0x42)
	body = append(body, wasmtest.SLEB64(int64(amd64DivBenchmarkAdd))...)
	body = append(body,
		0x7c, 0x21, 0x01, // i64.add; local.set accumulator
		0x20, 0x03, 0x41, 0x01, 0x6a, 0x21, 0x03, // counter++
		0x0c, 0x00, // continue
		0x0b, 0x0b,
		0x20, 0x01, // return accumulator
		0x0b,
	)
	funcType := wasmtest.FuncType(
		[]wasm.ValType{wasm.I32, wasm.I64, wasm.I64},
		[]wasm.ValType{wasm.I64},
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func amd64DivBenchmarkWantOne(tc amd64DivBenchmarkCase) uint64 {
	value := amd64DivBenchmarkSeed
	switch tc.opcode {
	case 0x7f: // i64.div_s
		value = uint64(int64(value) / int64(tc.divisor))
	case 0x80: // i64.div_u
		value /= tc.divisor
	case 0x81: // i64.rem_s
		value = uint64(int64(value) % int64(tc.divisor))
	case 0x82: // i64.rem_u
		value %= tc.divisor
	}
	return value + amd64DivBenchmarkAdd
}

// BenchmarkAMD64IntegerDivInstruction compares dynamic IDIV, constant-divisor
// magic multiplication, power-of-two shift lowering, and the common loop floor.
// One public invocation executes b.N dependent guest operations.
func BenchmarkAMD64IntegerDivInstruction(b *testing.B) {
	cases := []amd64DivBenchmarkCase{
		{name: "loop-control", magic: true},
		{name: "i64.div_s/dynamic-1", opcode: 0x7f, divisor: 1, magic: true, operations: 1},
		{name: "i64.div_s/const-1", opcode: 0x7f, constant: true, divisor: 1, magic: true, operations: 1},
		{name: "i64.div_s/dynamic-minus-1", opcode: 0x7f, divisor: ^uint64(0), magic: true, operations: 1},
		{name: "i64.div_s/const-minus-1", opcode: 0x7f, constant: true, divisor: ^uint64(0), magic: true, operations: 1},
		{name: "i64.rem_s/dynamic-1", opcode: 0x81, divisor: 1, magic: true, operations: 1},
		{name: "i64.rem_s/const-1", opcode: 0x81, constant: true, divisor: 1, magic: true, operations: 1},
		{name: "i64.rem_s/dynamic-minus-1", opcode: 0x81, divisor: ^uint64(0), magic: true, operations: 1},
		{name: "i64.rem_s/const-minus-1", opcode: 0x81, constant: true, divisor: ^uint64(0), magic: true, operations: 1},
		{name: "i64.div_u/dynamic-3", opcode: 0x80, divisor: 3, magic: true, operations: 1},
		{name: "i64.div_u/const-3-magic", opcode: 0x80, constant: true, divisor: 3, magic: true, operations: 1},
		{name: "i64.div_u/const-3-idiv", opcode: 0x80, constant: true, divisor: 3, magic: false, operations: 1},
		{name: "i64.div_u/const-8-shift", opcode: 0x80, constant: true, divisor: 8, magic: true, operations: 1},
		{name: "i64.rem_u/dynamic-3", opcode: 0x82, divisor: 3, magic: true, operations: 1},
		{name: "i64.rem_u/const-3-magic", opcode: 0x82, constant: true, divisor: 3, magic: true, operations: 1},
		{name: "i64.rem_u/const-3-idiv", opcode: 0x82, constant: true, divisor: 3, magic: false, operations: 1},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			cfg := NewRuntimeConfig().WithOptimization("magic-div", tc.magic)
			compiled, err := Compile(cfg, amd64DivInstructionBenchmarkModule(tc))
			if err != nil {
				b.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer instance.Close()

			got, err := instance.Invoke("run", 1, amd64DivBenchmarkSeed, tc.divisor)
			if err != nil || len(got) != 1 || got[0] != amd64DivBenchmarkWantOne(tc) {
				b.Fatalf("warm run = %v, %v; want [%d]", got, err, amd64DivBenchmarkWantOne(tc))
			}
			if uint64(b.N) > uint64(^uint32(0)) {
				b.Fatalf("iteration count %d exceeds the i32 guest-loop limit", b.N)
			}
			b.ReportAllocs()
			b.ResetTimer()
			got, err = instance.Invoke("run", uint64(uint32(b.N)), amd64DivBenchmarkSeed, tc.divisor)
			b.StopTimer()
			if err != nil || len(got) != 1 {
				b.Fatalf("run(%d) = %v, %v", b.N, got, err)
			}
			benchUintSink = got[0]
			if tc.operations != 0 {
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*int(tc.operations)), "ns/instruction")
			}
		})
	}
}

type amd64ShiftBenchmarkCase struct {
	name       string
	opcode     byte
	constant   bool
	count      uint64
	bmi2       bool
	operations int
}

func amd64ShiftInstructionBenchmarkModule(tc amd64ShiftBenchmarkCase) []byte {
	body := []byte{
		0x01, 0x01, 0x7f, // local 3: i32 counter
		0x02, 0x40, // block
		0x03, 0x40, // loop
		0x20, 0x03, 0x20, 0x00, 0x4f, 0x0d, 0x01, // if counter >= iterations, break
		0x20, 0x01, // accumulator
	}
	if tc.opcode != 0 {
		if tc.constant {
			body = append(body, 0x42)
			body = append(body, wasmtest.SLEB64(int64(tc.count))...)
		} else {
			body = append(body, 0x20, 0x02)
		}
		body = append(body, tc.opcode)
	}
	body = append(body, 0x42)
	body = append(body, wasmtest.SLEB64(int64(amd64DivBenchmarkAdd))...)
	body = append(body,
		0x7c, 0x21, 0x01, // i64.add; local.set accumulator
		0x20, 0x03, 0x41, 0x01, 0x6a, 0x21, 0x03, // counter++
		0x0c, 0x00, // continue
		0x0b, 0x0b,
		0x20, 0x01,
		0x0b,
	)
	funcType := wasmtest.FuncType(
		[]wasm.ValType{wasm.I32, wasm.I64, wasm.I64},
		[]wasm.ValType{wasm.I64},
	)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(funcType)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", byte(wasm.ExternFunc), 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func amd64ShiftBenchmarkWantOne(tc amd64ShiftBenchmarkCase) uint64 {
	value := amd64DivBenchmarkSeed
	switch tc.opcode {
	case 0x86:
		value <<= tc.count & 63
	case 0x8a:
		value = bits.RotateLeft64(value, -int(tc.count&63))
	}
	return value + amd64DivBenchmarkAdd
}

// BenchmarkAMD64IntegerShiftInstruction compares immediate, CL, and BMI2 rotate
// forms, including masked zero counts. One invocation executes b.N guest shifts.
func BenchmarkAMD64IntegerShiftInstruction(b *testing.B) {
	cases := []amd64ShiftBenchmarkCase{
		{name: "loop-control"},
		{name: "i64.rotr/dynamic-7", opcode: 0x8a, count: 7, operations: 1},
		{name: "i64.rotr/const-7-baseline", opcode: 0x8a, constant: true, count: 7, operations: 1},
		{name: "i64.rotr/const-7-rorx", opcode: 0x8a, constant: true, count: 7, bmi2: true, operations: 1},
		{name: "i64.rotr/const-0-baseline", opcode: 0x8a, constant: true, operations: 1},
		{name: "i64.rotr/const-0-rorx", opcode: 0x8a, constant: true, bmi2: true, operations: 1},
		{name: "i64.shl/const-0", opcode: 0x86, constant: true, operations: 1},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			cfg := NewRuntimeConfig().WithOptimization("bmi2-rorx", tc.bmi2)
			compiled, err := Compile(cfg, amd64ShiftInstructionBenchmarkModule(tc))
			if err != nil {
				b.Fatal(err)
			}
			defer compiled.Close()
			instance, err := Instantiate(compiled, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer instance.Close()

			got, err := instance.Invoke("run", 1, amd64DivBenchmarkSeed, tc.count)
			if err != nil || len(got) != 1 || got[0] != amd64ShiftBenchmarkWantOne(tc) {
				b.Fatalf("warm run = %v, %v; want [%d]", got, err, amd64ShiftBenchmarkWantOne(tc))
			}
			if uint64(b.N) > uint64(^uint32(0)) {
				b.Fatalf("iteration count %d exceeds the i32 guest-loop limit", b.N)
			}
			b.ReportAllocs()
			b.ResetTimer()
			got, err = instance.Invoke("run", uint64(uint32(b.N)), amd64DivBenchmarkSeed, tc.count)
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
