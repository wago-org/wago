//go:build (linux || darwin || windows) && arm64

package arm64

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
)

func TestInlineIntegerTupleArgumentsPreserveTrapOrder(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, bt, k, divS, divU, eqz, sub, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x6d), byte(0x6e), byte(0x45), byte(0x6b), byte(0x6a)
		minimum := uint64(0x80000000)
		if wide {
			typ, bt, k, divS, divU, eqz, sub, add = wasm.I64, 0x7e, 0x42, 0x7f, 0x80, 0x50, 0x7d, 0x7c
			minimum = 0x8000000000000000
		}
		caller := []byte{1, 1, bt, 0x20, 0, 0x20, 1, divS, 0x20, 0, 0x20, 2, divU,
			0x10, 1, add, 0x21, 3, 0x20, 3, 0x0b}
		helper := []byte{0, 0x02, 0x40, 0x03, 0x40, 0x20, 0, eqz, 0x0d, 1,
			0x20, 0, k, 1, sub, 0x21, 0, 0x20, 1, k, 1, add, 0x21, 1,
			0x0c, 0, 0x0b, 0x0b, 0x20, 0, 0x20, 1, 0x0b}
		m := modFuncs(t,
			funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, caller},
			funcDef{[]wasm.ValType{typ, typ}, []wasm.ValType{typ, typ}, helper})
		for _, enabled := range []bool{false, true} {
			for _, workers := range []int{1, 4} {
				var stats ModuleStats
				opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline-integer-tuples": enabled}}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, err := runArm64WrapperWithOptions(t, m, opts, 17, 5, 3)
				if err != nil || got != 8 {
					t.Fatalf("wide=%t enabled=%t workers=%d got=%d err=%v", wide, enabled, workers, got, err)
				}
				if diagnosticsEnabled && (stats.Funcs[0].Calls[callKindInline] == 1) != (enabled && !profileEnabled) {
					t.Fatal("did not exercise expanded tuple inlining")
				}
				for _, tc := range []struct {
					args [3]uint64
					code rt.TrapCode
				}{{[3]uint64{minimum, ^uint64(0), 0}, rt.TrapDivOverflow}, {[3]uint64{17, 5, 0}, rt.TrapDivZero}} {
					_, err := runArm64WrapperWithOptions(t, m, opts, tc.args[:]...)
					var trap *rt.TrapError
					if !errors.As(err, &trap) || trap.Code != tc.code {
						t.Fatalf("wide=%t enabled=%t workers=%d args=%x err=%v want trap=%v", wide, enabled, workers, tc.args, err, tc.code)
					}
				}
			}
		}
	}
}

func TestInlineIntegerTupleLoopRebindsAndZerosPrivateLocals(t *testing.T) {
	savedLimit := inlineTupleMaxBytes
	t.Cleanup(func() { inlineTupleMaxBytes = savedLimit })
	for _, wide := range []bool{false, true} {
		typ, bt, k, eqz, sub, add := wasm.I32, byte(0x7f), byte(0x41), byte(0x45), byte(0x6b), byte(0x6a)
		fold := func(v uint64) uint64 { return uint64(uint32(v)) }
		if wide {
			typ, bt, k, eqz, sub, add = wasm.I64, 0x7e, 0x42, 0x50, 0x7d, 0x7c
			fold = func(v uint64) uint64 { return v }
		}
		for _, variant := range []string{"call-free", "call-free-disabled", "remaining-call", "option-disabled", "body-limit", "float-local", "many-locals", "large-body"} {
			inlineTupleMaxBytes = savedLimit
			if variant == "body-limit" {
				inlineTupleMaxBytes = min(16, savedLimit)
			}
			caller := []byte{1, 1, bt, 0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, add, 0x21, 3,
				0x20, 0, 0x20, 1, 0x20, 2, 0x10, 1, add, 0x20, 3, add, 0x0b}
			// Private local5 counts iterations from zero on EACH splice. The loop
			// rotates two other private homes; both original caller arguments survive.
			helper := []byte{1, 3, bt, 0x20, 0, 0x20, 1, 0x20, 2, 0x03, 1,
				0x21, 4, 0x21, 3, 0x21, 0, 0x20, 0, eqz, 0x04, 2,
				0x20, 3, 0x20, 4, 0x20, 5, add, 0x05,
				0x20, 5, k, 1, add, 0x21, 5,
				0x20, 0, k, 1, sub, 0x20, 4, 0x20, 3, 0x20, 4, add, 0x0c, 1,
				0x0b, 0x0b, 0x0b}
			switch variant {
			case "float-local":
				helper = append([]byte{2, 3, bt, 1, 0x7c}, helper[3:]...)
			case "many-locals":
				helper[1] = 5
			case "large-body":
				extra := make([]byte, 30)
				for i := range extra {
					extra[i] = 0x01
				}
				helper = append(append(append([]byte{}, helper[:3]...), extra...), helper[3:]...)
			}
			if variant == "remaining-call" {
				caller = append(append(append([]byte{}, caller[:14]...), 0x10, 3), caller[14:]...)
			}
			opaque := []byte{0}
			for range 20 {
				opaque = append(opaque, 0x01)
			}
			opaque = append(opaque, 0x0b)
			m := modFuncs(t,
				funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ}, caller},
				funcDef{[]wasm.ValType{typ, typ, typ}, []wasm.ValType{typ, typ}, helper},
				funcDef{nil, []wasm.ValType{typ, typ}, []byte{0, k, 0, k, 0, 0x0b}}, funcDef{nil, nil, opaque})
			for _, enabled := range []bool{false, true} {
				for _, workers := range []int{1, 4} {
					for _, count := range []uint64{0, 1, 2, 7, 12} {
						for _, input := range [][2]uint64{{3, 7}, {0xfffffff0, 0x80000001}, {^uint64(0), 0x8000000000000001}} {
							a, b := fold(input[0]), fold(input[1])
							for range count {
								a, b = b, fold(a+b)
							}
							want := fold(2 * fold(a+b+count))
							var stats ModuleStats
							opts := CompileOptions{Workers: workers, Optimizations: map[string]bool{"inline": enabled, "inline-callfree": variant != "call-free-disabled", "inline-integer-tuples": variant != "option-disabled"}}
							if diagnosticsEnabled {
								opts.Stats = &stats
							}
							got, err := runArm64WrapperWithOptions(t, m, opts, count, input[0], input[1])
							got = fold(got)
							if err != nil || got != want {
								t.Fatalf("wide=%t inline=%t workers=%d count=%d input=%x got=%x err=%v want=%x", wide, enabled, workers, count, input, got, err, want)
							}
							if diagnosticsEnabled && (stats.Funcs[0].Calls[callKindInline] == 2) != (enabled && !profileEnabled && variant == "call-free") {
								t.Fatalf("inline=%t calls=%v", enabled, stats.Funcs[0].Calls)
							}
							if diagnosticsEnabled && (stats.Funcs[0].Peephole["loop-local-params"] == 2) != (enabled && !profileEnabled && variant == "call-free") {
								t.Fatalf("inline=%t retained parameter loops=%d", enabled, stats.Funcs[0].Peephole["loop-local-params"])
							}
						}
					}
				}
			}
		}
	}
}
