//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"fmt"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func controlDepthSource(depth int, kind string, swar bool) []byte {
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

func TestProductionControlDepthInlineAndGrowthBeforePublication(t *testing.T) {
	for _, swar := range []bool{false, true} {
		for _, kind := range []string{"branch", "loop", "dead", "unreachable"} {
			for _, depth := range []int{128, 129, 257} {
				t.Run(fmt.Sprintf("swar%t-%s-%d", swar, kind, depth), func(t *testing.T) {
					source := controlDepthSource(depth, kind, swar)
					m, err := wasm.DecodeModule(source)
					if err != nil {
						t.Fatal(err)
					}
					if err := wasm.ValidateModule(m); err != nil {
						t.Fatal("fixture must be valid Wasm regardless of backend bound", err)
					}
					pressure := 0
					image, err := CompileModuleWith(m, CompileOptions{DeferCodeMapping: true, Interruptible: true, MemoryPressure: func() { pressure++ }})
					if err != nil || image == nil || len(image.Code) == 0 || image.CodeImage != nil || pressure != 1 {
						t.Fatal("deep control production image", depth, err, pressure)
					}
					if len(image.Code) > MaxCodeBytes {
						t.Fatal("control depth escaped aggregate code budget")
					}
				})
			}
		}
	}
}

func TestControlDepthFixedScratchBound(t *testing.T) {
	// Depth is compiler bookkeeping. It must not turn into per-function guest
	// locals or an unbounded heap-backed frame stack.
	integerBytes := unsafe.Sizeof([1 + maxControlNesting]integerControl{})
	swarBytes := unsafe.Sizeof([1 + maxControlNesting]swarControl{})
	if integerBytes > 24<<10 || swarBytes > 24<<10 {
		t.Fatal("fixed compiler control scratch exceeded reviewed ceiling", integerBytes, swarBytes)
	}
	t.Logf("%d-frame compiler scratch: integer=%d bytes SWAR=%d bytes", 1+maxControlNesting, integerBytes, swarBytes)
	for _, kind := range []string{"branch", "loop", "dead", "unreachable"} {
		var shallow, deep integerSummary
		for _, depth := range []int{1, 128} {
			m, err := wasm.DecodeModule(controlDepthSource(depth, kind, false))
			if err != nil {
				t.Fatal(err)
			}
			ft, ok := m.LocalFuncType(0)
			if !ok {
				t.Fatal("signature")
			}
			summary, err := walkInteger(&m.Code[0], ft, &moduleState{production: true}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if depth == 1 {
				shallow = summary
			} else {
				deep = summary
			}
		}
		if shallow.maxStack != deep.maxStack {
			t.Fatal("control nesting enlarged live operand homes", kind, shallow.maxStack, deep.maxStack)
		}
	}
}
