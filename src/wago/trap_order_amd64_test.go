//go:build (linux || darwin || windows) && amd64 && !tinygo

package wago

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// trapOrderModule leaves both quotients deferred until the named consumer.
// The control/call cases leave the first quotient below the condition/argument.
func trapOrderModule(wide bool, consumer string) []byte {
	typ, valueType, div, add, constant := wasm.I32, byte(0x7f), byte(0x6d), byte(0x6a), byte(0x41)
	if wide {
		typ, valueType, div, add, constant = wasm.I64, 0x7e, 0x7f, 0x7c, 0x42
	}
	var body []byte
	if consumer == "br_if" {
		body = append(body, 0x02, valueType) // block (result i32/i64)
	}
	body = append(body, 0x20, 0, 0x20, 1, div, 0x20, 2, 0x20, 3, div)
	switch consumer {
	case "add", "sub", "mul", "div":
		offset := map[string]byte{"add": 0, "sub": 1, "mul": 2, "div": 3}[consumer]
		body = append(body, add+offset)
	case "if", "br_if":
		if wide {
			body = append(body, 0xa7) // i32.wrap_i64 condition, still deferred
		}
		if consumer == "if" {
			body = append(body, 0x04, valueType, constant, 5, 0x05, constant, 9, 0x0b, add)
		} else {
			body = append(body, 0x0d, 0, constant, 9, add, 0x0b)
		}
	case "inline_add_drop":
		body = append(body, 0x10, 1, 0x1a)
	case "call", "call_noinline", "mixed_call":
		if consumer == "mixed_call" {
			body = append(body, 0x44, 0, 0, 0, 0, 0, 0, 0, 0) // f64.const 0
		}
		body = append(body, 0x10, 1, add)
	default:
		panic("unknown trap-order consumer")
	}
	body = append(body, 0x0b)
	types := [][]byte{wasmtest.FuncType([]wasm.ValType{typ, typ, typ, typ}, []wasm.ValType{typ})}
	functions := [][]byte{wasmtest.ULEB(0)}
	codes := [][]byte{wasmtest.Code(body)}
	if consumer == "call" || consumer == "call_noinline" || consumer == "mixed_call" || consumer == "inline_add_drop" {
		params := []wasm.ValType{typ}
		if consumer == "mixed_call" {
			params = append(params, wasm.F64)
		}
		types = append(types, wasmtest.FuncType(params, []wasm.ValType{typ}))
		functions = append(functions, wasmtest.ULEB(1))
		// A loop (with no backedge) excludes the callee from inline candidates.
		block := byte(0x02)
		if consumer == "call_noinline" {
			block = 0x03
		}
		callee := []byte{block, valueType, 0x20, 0, 0x0b, 0x0b}
		if consumer == "inline_add_drop" {
			callee = []byte{0x20, 0, constant, 1, add, 0x0b}
		}
		codes = append(codes, wasmtest.Code(callee))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, wasmtest.Vec(functions...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(codes...)),
	)
}

func compileTrapOrder(t testing.TB, module []byte) (*Compiled, *Instance) {
	t.Helper()
	compiled, err := Compile(nil, module)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { compiled.Close() })
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { instance.Close() })
	return compiled, instance
}

func TestTrapOrderAMD64(t *testing.T) {
	for _, wide := range []bool{false, true} {
		min, minusOne := uint64(1<<31), uint64(0xffffffff)
		if wide {
			min, minusOne = 1<<63, ^uint64(0)
		}
		for _, consumer := range []string{"add", "sub", "mul", "div", "if", "br_if", "call", "call_noinline", "mixed_call", "inline_add_drop"} {
			t.Run(fmt.Sprintf("wide=%v/%s", wide, consumer), func(t *testing.T) {
				_, instance := compileTrapOrder(t, trapOrderModule(wide, consumer))
				for _, tc := range []struct {
					name string
					args [4]uint64
					want TrapCode
				}{
					{"first_overflow", [4]uint64{min, minusOne, 1, 0}, TrapDivOverflow},
					{"first_zero", [4]uint64{1, 0, min, minusOne}, TrapDivZero},
					{"only_first", [4]uint64{1, 0, 6, 2}, TrapDivZero},
					{"only_second", [4]uint64{6, 2, min, minusOne}, TrapDivOverflow},
				} {
					t.Run(tc.name, func(t *testing.T) {
						_, err := instance.Invoke("run", tc.args[:]...)
						var trap *TrapError
						if !errors.As(err, &trap) || trap.Code != tc.want {
							t.Fatalf("Invoke(%v) = %v; want %v", tc.args, err, tc.want)
						}
					})
				}
				// Successful quotients also exercise register ownership across the
				// changed ordering, and both outcomes of each control condition.
				for _, right := range []uint64{2, 0} {
					want := uint64(3)
					switch consumer {
					case "add", "call", "call_noinline", "mixed_call":
						want += right
					case "sub":
						want -= right
					case "mul":
						want *= right
					case "div":
						if right == 0 {
							continue
						}
						want /= right
					case "if":
						if right != 0 {
							want += 5
						} else {
							want += 9
						}
					case "br_if":
						if right == 0 {
							want += 9
						}
					}
					got, err := instance.Invoke("run", 6, 2, right*3, 3)
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("right quotient %d: got %v, %v; want %d", right, got, err, want)
					}
				}
			})
		}
	}
}

func BenchmarkCompileTrapOrderAMD64(b *testing.B) {
	for _, consumer := range []string{"add", "if", "call_noinline"} {
		b.Run(consumer, func(b *testing.B) {
			module := trapOrderModule(false, consumer)
			var codeBytes int
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				compiled, err := Compile(nil, module)
				if err != nil {
					b.Fatal(err)
				}
				codeBytes = compiled.CodeSize()
				compiled.Close()
			}
			b.ReportMetric(float64(codeBytes), "code-B")
		})
	}
}

// Benchmark successful inputs whose results are correct before and after the fix.
func BenchmarkInvokeTrapOrderAMD64(b *testing.B) {
	for _, consumer := range []string{"add", "if", "call_noinline"} {
		b.Run(consumer, func(b *testing.B) {
			compiled, instance := compileTrapOrder(b, trapOrderModule(false, consumer))
			want := uint64(12)
			if consumer == "if" {
				want = 11
			}
			got, err := instance.Invoke("run", 42, 7, 18, 3)
			if err != nil || len(got) != 1 || got[0] != want {
				b.Fatalf("got %v, %v; want %d", got, err, want)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := instance.Invoke("run", 42, 7, 18, 3); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(compiled.CodeSize()), "code-B")
		})
	}
}
