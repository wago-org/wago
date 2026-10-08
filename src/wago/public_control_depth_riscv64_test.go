//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"os"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ControlDepthSource(depth int, kind string, swar bool) []byte {
	types := [][]byte{wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32})}
	if swar {
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))
	}
	body := []byte{}
	if kind == "dead" {
		body = append(body, 0x20, 0, 0x0f)
	}
	for i := 0; i < depth; i++ {
		op, result := byte(0x02), byte(0x40)
		if kind == "branch" {
			result = 0x7f
		}
		if kind == "loop" && i == depth-1 {
			op = 0x03
		}
		body = append(body, op, result)
	}
	switch kind {
	case "branch":
		body = append(body, 0x20, 0, 0x0c)
		body = append(body, wasmtest.ULEB(uint32(depth-1))...)
	case "loop":
		body = append(body, 0x20, 1, 0x41, 1, 0x6b, 0x22, 1, 0x0d, 0)
	case "dead", "unreachable":
		body = append(body, 0x00)
	default:
		panic("unknown depth fixture")
	}
	for i := 0; i < depth; i++ {
		body = append(body, 0x0b)
	}
	if kind == "loop" || kind == "unreachable" {
		body = append(body, 0x20, 0)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func TestRV64PublicControlDepth128(t *testing.T) {
	for _, swar := range []bool{false, true} {
		for _, kind := range []string{"branch", "loop", "dead", "unreachable"} {
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("swar%t-%s-artifact%t", swar, kind, artifact), func(t *testing.T) {
					c := rv64TableStageCompile(t, fmt.Sprintf("control-depth128-%t-%s", swar, kind), rv64ControlDepthSource(128, kind, swar), artifact)
					in := rv64MMInstance(t, c)
					if kind == "unreachable" {
						got, err := in.Invoke("run", 0xdeadbeef80000001, 3)
						var trap *coreruntime.TrapError
						if got != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 0 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 257 {
							t.Fatal("deep unreachable source/result", got, err)
						}
						return
					}
					for _, count := range []uint64{1, 3} {
						rv64MMCall(t, in, "run", []uint64{0x80000001}, 0xdeadbeef80000001, 0xaabbccdd00000000|count)
					}
				})
			}
		}
	}
}

func TestRV64PublicControlDepth129Rejects(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source admission requires compiler")
	}
	for _, swar := range []bool{false, true} {
		for _, kind := range []string{"branch", "loop", "dead", "unreachable"} {
			t.Run(fmt.Sprintf("swar%t-%s", swar, kind), func(t *testing.T) {
				c, err := Compile(nil, rv64ControlDepthSource(129, kind, swar))
				if c != nil {
					_ = c.Close()
				}
				if !errors.Is(err, railshot.ErrUnsupported) {
					t.Fatal("depth129 did not fail closed", err)
				}
			})
		}
	}
}

func TestRV64PublicControlDepth128MixedBranchValues(t *testing.T) {
	for _, vector := range []bool{false, true} {
		types := []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
		input := []uint64{0xdeadbeef80000001, 0xfeedface7fa00001, 0x80000000deadbeef, 0x7ff0000000000001}
		want := []uint64{0x80000001, 0x7fa00001, 0x80000000deadbeef, 0x7ff0000000000001}
		if vector {
			types = append(types, wasm.V128)
			input = append(input, 0x8877665544332211, 0xffeeddccbbaa0099)
			want = append(want, 0x8877665544332211, 0xffeeddccbbaa0099)
		}
		body := []byte{}
		for range 128 {
			body = append(body, 0x02, 1)
		}
		for i := range types {
			body = append(body, 0x20, byte(i))
		}
		body = append(body, 0x0c, 127)
		for range 128 {
			body = append(body, 0x0b)
		}
		body = append(body, 0x0b)
		source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, types))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				c := rv64TableStageCompile(t, fmt.Sprintf("control-depth128-mixed-%t", vector), source, artifact)
				in := rv64MMInstance(t, c)
				rv64MMCall(t, in, "run", want, input...)
			})
		}
	}
}
