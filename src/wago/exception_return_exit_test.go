//go:build ((linux && (amd64 || arm64 || riscv64)) || (darwin && arm64)) && !tinygo

package wago

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// exceptionReturnExitModule builds $inner, which leaves `nesting` try_tables
// through a function exit, and two callers that throw after $inner returns.
// Every try in $inner catches tag 0 to $h. A handler left installed by that
// exit belongs to a returned frame, so the caller's later throw must neither
// land there nor skip the caller's own handler.
//
// Exits: "return"; "return_call" and "return_call_indirect" to $leaf;
// "return_call_host" to the imported identity function; and "catch_return",
// which returns from a catch target nested inside the tries.
// With hostCalls, each function calls the imported identity function first.
// Callers reach $inner by "call" or "call_indirect".
func exceptionReturnExitModule(exit, caller string, nesting int, hostCalls bool) []byte {
	tagSig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	i32Sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	base := byte(0)
	var imports []byte
	if hostCalls {
		base = 1
		imports = wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("id")...), 0x00, 1)))
	}
	throwFn, leafFn, innerFn := base, base+1, base+2
	arg := []byte{0x20, 0} // local.get 0, optionally passed through the host
	if hostCalls {
		arg = append(arg, 0x10, 0)
	}

	throwBody := append(append([]byte{}, arg...), 0x08, 0, 0x0b)
	leafBody := append(append([]byte{}, arg...), 0x0b)

	inner := []byte{0x02, 0x7f} // block $h (result i32)
	for i := range nesting {
		inner = append(inner, 0x1f, 0x40, 1, byte(wasm.CatchTag), 0, byte(i)) // try_table (catch 0 $h)
	}
	switch exit {
	case "return":
		inner = append(inner, arg...)
		inner = append(inner, 0x0f)
	case "return_call":
		inner = append(inner, arg...)
		inner = append(inner, 0x12, leafFn)
	case "return_call_host":
		inner = append(inner, 0x20, 0, 0x12, 0) // return_call $id
	case "return_call_indirect":
		inner = append(inner, arg...)
		inner = append(inner, 0x41, 1, 0x13, 1, 0) // table slot 1 holds $leaf
	case "catch_return":
		inner = append(inner, 0x02, 0x7f, 0x1f, 0x40, 1, byte(wasm.CatchTag), 0, 0) // block $in; try_table (catch 0 $in)
		inner = append(inner, arg...)
		inner = append(inner, 0x08, 0, 0x0b, 0x00, 0x0b, 0x0f) // throw; end; unreachable; end $in; return payload
	default:
		panic("unknown exit " + exit)
	}
	for range nesting {
		inner = append(inner, 0x0b)
	}
	// A catch by $h would return the payload plus 1000.
	inner = append(inner, 0x00, 0x0b, 0x41, 0xe8, 0x07, 0x6a, 0x0b)

	callInner := []byte{0x10, innerFn}
	if caller == "call_indirect" {
		callInner = []byte{0x41, 0, 0x11, 1, 0} // table slot 0 holds $inner
	}
	// run catches the payload of a throw after $inner returned; -1 means the
	// caller's handler was skipped.
	run := []byte{0x02, 0x7f, 0x1f, 0x40, 1, byte(wasm.CatchTag), 0, 0, 0x20, 0}
	run = append(run, callInner...)
	run = append(run, 0x1a, 0x20, 0, 0x10, throwFn, 0x0b, 0x41, 0x7f, 0x0f, 0x0b, 0x0b)
	// unhandled throws after $inner returned, outside any try in this frame.
	unhandled := []byte{0x20, 0}
	unhandled = append(unhandled, callInner...)
	unhandled = append(unhandled, 0x1a, 0x20, 0, 0x10, throwFn, 0x41, 0x7f, 0x0b)

	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(tagSig, i32Sig)),
		imports,
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{1}, []byte{1}, []byte{1}, []byte{1})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 2})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("inner", 0, uint32(innerFn)),
			wasmtest.ExportEntry("run", 0, uint32(base+3)),
			wasmtest.ExportEntry("unhandled", 0, uint32(base+4)),
		)),
		wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0, 0x0b, 2, innerFn, leafFn})),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(throwBody), wasmtest.Code(leafBody), wasmtest.Code(inner),
			wasmtest.Code(run), wasmtest.Code(unhandled),
		)),
	)
}

func TestExceptionReturnExitsDiscardHandlers(t *testing.T) {
	for _, exit := range []string{"return", "return_call", "return_call_indirect", "return_call_host", "catch_return"} {
		for _, caller := range []string{"call", "call_indirect"} {
			for nesting := 1; nesting <= 3; nesting++ {
				for _, hostCalls := range []bool{false, true} {
					if exit == "return_call_host" && !hostCalls {
						continue
					}
					for _, inline := range exceptionReturnExitInlineModes() {
						name := fmt.Sprintf("%s/%s/depth%d/host=%t/inline=%t", exit, caller, nesting, hostCalls, inline)
						t.Run(name, func(t *testing.T) {
							testExceptionReturnExit(t, exceptionReturnExitModule(exit, caller, nesting, hostCalls), hostCalls, inline)
						})
					}
				}
			}
		}
	}
}

// The RV64 backend has no inliner and rejects the option.
func exceptionReturnExitInlineModes() []bool {
	if runtime.GOARCH == "riscv64" {
		return []bool{false}
	}
	return []bool{false, true}
}

func testExceptionReturnExit(t *testing.T, data []byte, hostCalls, inline bool) {
	t.Helper()
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	if runtime.GOARCH != "riscv64" {
		cfg = cfg.WithOptimization("inline", inline)
	}
	c, err := Compile(cfg, data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	opts := InstantiateOptions{}
	if hostCalls {
		imports := NewImports()
		imports.HostFunc("env", "id", func(x int32) int32 { return x }).Params(ValI32).Results(ValI32)
		opts.Imports = imports
	}
	in, err := Instantiate(c, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	const arg = 42
	if got, err := in.Invoke("inner", arg); err != nil || len(got) != 1 || got[0] != arg {
		t.Fatalf("inner(%d) = %v, %v; want %d", arg, got, err, arg)
	}
	if got, err := in.Invoke("run", arg); err != nil || len(got) != 1 || got[0] != arg {
		t.Fatalf("run(%d) = %v, %v; want the caller to catch %d", arg, got, err, arg)
	}
	if got, err := in.Invoke("unhandled", arg); err == nil || !strings.Contains(err.Error(), "unhandled WebAssembly exception") {
		t.Fatalf("unhandled(%d) = %v, %v; want unhandled exception", arg, got, err)
	}
	// The instance must stay usable after the unhandled exception.
	if got, err := in.Invoke("run", arg+1); err != nil || len(got) != 1 || got[0] != arg+1 {
		t.Fatalf("run(%d) after unhandled = %v, %v; want %d", arg+1, got, err, arg+1)
	}
}

// exceptionReturnShapeModule builds $inner, which returns from inside two
// try_tables with the given result shape, and $run, which throws its argument
// after $inner returned and catches it itself. A large frame places the
// handler records beyond the directly addressable stack displacement.
func exceptionReturnShapeModule(shape string, largeFrame bool) []byte {
	tagSig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil)
	runSig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	var innerSig, ret, drop, fallback []byte
	switch shape {
	case "i32":
		innerSig = runSig
		ret, drop, fallback = []byte{0x20, 0}, []byte{0x1a}, []byte{0x41, 0x7f}
	case "f64":
		innerSig = wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.F64})
		ret, drop = []byte{0x20, 0, 0xb7}, []byte{0x1a} // f64.convert_i32_s
		fallback = []byte{0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0xbf}
	case "i32x2":
		innerSig = wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32})
		ret, drop, fallback = []byte{0x20, 0, 0x20, 0}, []byte{0x1a, 0x1a}, []byte{0x41, 0x7f, 0x41, 0x7f}
	default:
		panic("unknown shape " + shape)
	}

	throwBody := []byte{0x20, 0, 0x08, 0, 0x0b}
	inner := []byte{0x02, 0x7f}                                     // block $h (result i32)
	inner = append(inner, 0x1f, 0x40, 1, byte(wasm.CatchTag), 0, 0) // try_table (catch 0 $h)
	inner = append(inner, 0x1f, 0x40, 1, byte(wasm.CatchTag), 0, 1)
	inner = append(inner, ret...)
	inner = append(inner, 0x0f, 0x0b, 0x0b, 0x00, 0x0b, 0x1a) // return; end; end; unreachable; end $h; drop payload
	inner = append(inner, fallback...)
	inner = append(inner, 0x0b)
	innerCode := wasmtest.Code(inner)
	if largeFrame {
		// 5000 i64 locals: 40000 bytes of local slots.
		fn := append([]byte{1}, wasmtest.ULEB(5000)...)
		fn = append(fn, 0x7e)
		fn = append(fn, inner...)
		innerCode = append(wasmtest.ULEB(uint32(len(fn))), fn...)
	}
	run := []byte{0x02, 0x7f, 0x1f, 0x40, 1, byte(wasm.CatchTag), 0, 0, 0x20, 0, 0x10, 1}
	run = append(run, drop...)
	run = append(run, 0x20, 0, 0x10, 0, 0x0b, 0x41, 0x7f, 0x0f, 0x0b, 0x0b)

	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(tagSig, runSig, innerSig)),
		wasmtest.Section(3, wasmtest.Vec([]byte{0}, []byte{2}, []byte{1})),
		wasmtest.Section(13, wasmtest.Vec([]byte{0, 0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(throwBody), innerCode, wasmtest.Code(run))),
	)
}

func TestExceptionReturnExitShapesDiscardHandlers(t *testing.T) {
	for _, shape := range []string{"i32", "f64", "i32x2"} {
		for _, largeFrame := range []bool{false, true} {
			for _, inline := range exceptionReturnExitInlineModes() {
				t.Run(fmt.Sprintf("%s/large=%t/inline=%t", shape, largeFrame, inline), func(t *testing.T) {
					cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
					if runtime.GOARCH != "riscv64" {
						cfg = cfg.WithOptimization("inline", inline)
					}
					c, err := Compile(cfg, exceptionReturnShapeModule(shape, largeFrame))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					in, err := Instantiate(c, InstantiateOptions{})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					for _, arg := range []uint64{0, 42} {
						if got, err := in.Invoke("run", arg); err != nil || len(got) != 1 || got[0] != arg {
							t.Fatalf("run(%d) = %v, %v; want the caller to catch %d", arg, got, err, arg)
						}
					}
				})
			}
		}
	}
}
