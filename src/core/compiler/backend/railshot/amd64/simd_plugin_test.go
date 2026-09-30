//go:build linux && amd64 && !tinygo

package amd64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	plugincodegen "github.com/wago-org/wago/codegen/amd64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/plugins"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"golang.org/x/sys/cpu"
)

func TestSIMDLaneFallbackPreservesPluginYMM(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("plugin execution requires AVX2")
	}
	typ, err := plugins.PrepareCustomType(plugins.CustomTypeSpec{Name: "test.vector", Size: 32, Carrier: plugins.WasmI32})
	if err != nil {
		t.Fatal(err)
	}
	const lo, hi = uint64(0x0123456789abcdef), uint64(0xfedcba9876543210)
	producer := &plugincodegen.Lowering{
		Compatibility: plugincodegen.CompatibilityManaged, Features: plugincodegen.FeatureAVX2,
		Managed: func(ctx plugincodegen.ManagedContext) error {
			return ctx.OutputCustom(ctx.ConstYMMRepeated128(lo, hi))
		},
	}
	consumer := &plugincodegen.Lowering{
		Compatibility: plugincodegen.CompatibilityFullAccess, Features: plugincodegen.FeatureAVX2,
		Emit: func(ctx plugincodegen.Context) error {
			regs, err := ctx.InputCustom(0)
			if err != nil {
				return err
			}
			ctx.Encoder().YMovdquStoreDisp(ctx.MemoryBase(), 0, regs[0])
			ctx.ReleaseVector(regs[0])
			return nil
		},
	}
	importFunc := func(name string, idx uint32) []byte {
		out := append(wasmtest.Name("test"), wasmtest.Name(name)...)
		return append(append(out, 0), wasmtest.ULEB(idx)...)
	}
	var want [32]byte
	for i := 0; i < len(want); i += 16 {
		binary.LittleEndian.PutUint64(want[i:], lo)
		binary.LittleEndian.PutUint64(want[i+8:], hi)
	}
	vector := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	for _, width := range []int{4, 8} {
		for lane := 1; lane < 16/width; lane++ {
			for _, sse41 := range []bool{false, true} {
				t.Run(fmt.Sprintf("width=%d/lane=%d/sse41=%v", width, lane, sse41), func(t *testing.T) {
					if sse41 && !cpu.X86.HasSSE41 {
						t.Skip("native lane extraction requires SSE4.1")
					}
					// Keep a custom YMM value live beneath an ordinary Wasm lane
					// extraction, then store all 256 bits after the scratch restore.
					body := []byte{1, 1, 0x7e, 0x10, 0} // local i64; call produce
					body = append(body, v128ConstBytes(vector)...)
					op, extracted := uint32(29), binary.LittleEndian.Uint64(vector[8:])
					if width == 4 {
						op, extracted = 27, uint64(binary.LittleEndian.Uint32(vector[lane*4:]))
					}
					body = append(body, simdOp(op)...)
					body = append(body, byte(lane))
					if width == 4 {
						body = append(body, 0xad) // i64.extend_i32_u
					}
					body = append(body, 0x21, 0, 0x10, 1, 0x20, 0, 0x0b) // save lane; consume; return lane
					m, err := wasm.DecodeModule(wasmtest.Module(
						wasmtest.Section(1, wasmtest.Vec(
							wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
							wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil),
							wasmtest.FuncType(nil, []wasm.ValType{wasm.I64}))),
						wasmtest.Section(2, wasmtest.Vec(importFunc("produce", 0), importFunc("consume", 1))),
						wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(2))),
						wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
						wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 2))),
						wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
					))
					if err != nil {
						t.Fatal(err)
					}
					features := shared.AMD64AVX | shared.AMD64AVX2
					if sse41 {
						features |= shared.AMD64SSE41
					}
					opts := CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, CustomInstructions: map[uint32]CustomInstruction{
						0: {Codegen: producer, ResultWidth: 256, CustomOutput: &typ},
						1: {Codegen: consumer, InputWidths: []int32{256}, CustomInputs: []plugins.CustomType{typ}},
					}}
					got, mem, err := runMemAmd64WithOptions(t, m, opts, nil)
					if err != nil {
						t.Fatal(err)
					}
					if got != extracted {
						t.Fatalf("extracted lane = %#x, want %#x", got, extracted)
					}
					if !bytes.Equal(mem[:32], want[:]) {
						t.Fatalf("live plugin vector = %x, want %x", mem[:32], want)
					}
				})
			}
		}
	}
}
