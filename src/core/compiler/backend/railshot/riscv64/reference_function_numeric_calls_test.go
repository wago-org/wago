//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func referenceNumericCallsDecode(t *testing.T, vector, tails, wide bool, start, target int) *wasm.Module {
	t.Helper()
	m, err := wasm.DecodeModule(wasmtest.ClosedReferenceNumericCalls(vector, tails, wide, start, target))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestReferenceFunctionNumericCallsSource(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, tails := range []bool{false, true} {
			for _, wide := range []bool{false, true} {
				for _, start := range []int{0, 1, 2} {
					for _, target := range []int{0, 1, 2} {
						t.Run(fmt.Sprintf("vector%t-tails%t-wide%t-start%d-target%d", vector, tails, wide, start, target), func(t *testing.T) {
							m := referenceNumericCallsDecode(t, vector, tails, wide, start, target)
							if err := admitClosedReferenceFunctionModule(m, vector); err != nil {
								t.Fatal(err)
							}
							o := closedReferenceProductionOptions(tails, true)
							o.ImportBindings = hostImportBindings(3)
							image, err := CompileModuleWith(m, o)
							if err != nil || image == nil || !image.NativeReferenceCalls || !image.NativeReferenceFunctions || !image.NativeReferenceControl || len(image.Entry) != 10 {
								t.Fatal("numeric call_ref staging", err)
							}
							if (image.NativeTail != nil) != tails || tails && len(image.NativeTail.Plans) != 1 {
								t.Fatal("ordinary references expanded tail plans")
							}
							for _, missing := range []string{"call", "function", "invocation", "interruptible", "foreign"} {
								broken := o
								switch missing {
								case "call":
									broken.NativeReferenceCalls = false
								case "function":
									broken.NativeReferenceFunctions = false
								case "invocation":
									broken.InvocationContext = false
								case "interruptible":
									broken.Interruptible = false
								case "foreign":
									broken.ForeignTableCalls = false
								}
								pressure := 0
								broken.MemoryPressure = func() { pressure++ }
								if got, err := CompileModuleWith(m, broken); got != nil || err == nil || pressure != 0 {
									t.Fatal("missing permission reached staging", missing, err, pressure)
								}
							}
						})
					}
				}
			}
		}
	}
}

func TestReferenceFunctionNumericCallsConfinement(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for name, body := range map[string][]byte{
			"private-dead-type": {0, 0x14, 2, 0x0b}, "missing-dead-type": {0, 0x14, 127, 0x0b},
			"private-dead-target": {0, 0xd2, 12, 0x14, 0, 0x0b}, "indirect": {0, 0x11, 0, 0, 0x0b},
			"reference-tail": {0, 0x15, 0, 0x0b}, "indirect-tail": {0, 0x13, 0, 0, 0x0b},
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				m := referenceNumericCallsDecode(t, vector, true, true, 2, 0)
				m.Code[0].BodyBytes = body
				o := closedReferenceProductionOptions(true, true)
				o.ImportBindings = hostImportBindings(3)
				pressure := 0
				o.MemoryPressure = func() { pressure++ }
				if image, err := CompileModuleWith(m, o); image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
					t.Fatal("unconfined dead target reached allocation", err, pressure)
				}
			})
		}
	}
}

func TestReferenceFunctionNumericCallsScalarFloor(t *testing.T) {
	numeric := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.I32}, Results: []wasm.ValType{wasm.I32}}
	private := &wasm.CompType{Kind: wasm.CompFunc, Params: []wasm.ValType{wasm.FuncRef}}
	state := &moduleState{production: true, referenceFunctions: true, referenceCalls: true, importReferences: true, foreignTableCalls: true,
		functions: []functionSpec{{signature: private}}, types: []functionSpec{{signature: numeric, params: []bool{false}, results: []bool{false}}}}
	for _, tc := range []struct {
		name  string
		body  []byte
		valid bool
	}{
		{"live", []byte{0x41, 1, 0xd0, 0, 0x14, 0, 0x1a, 0x0b}, true},
		{"explicit-dead-missing", []byte{0, 0x14, 0, 0x1a, 0x0b}, true},
		{"unknown-specialization", []byte{0, 0x1b, 0x14, 0, 0x1a, 0x0b}, true},
		{"known-numeric-reference-dead", []byte{0, 0x41, 1, 0x14, 0, 0x1a, 0x0b}, false},
		{"known-reference-numeric-dead", []byte{0, 0xd0, 0x70, 0xd0, 0, 0x14, 0, 0x1a, 0x0b}, false},
		{"known-wide-numeric-dead", []byte{0, 0x42, 1, 0xd0, 0, 0x14, 0, 0x1a, 0x0b}, false},
		{"inherited-dead-new-floor", []byte{0, 2, 0x40, 0x14, 0, 0x1a, 0x0b, 0x0b}, false},
		{"explicit-nested-floor", []byte{0, 2, 0x40, 0, 0x14, 0, 0x1a, 0x0b, 0x0b}, true},
		{"live-underflow", []byte{0xd0, 0, 0x14, 0, 0x1a, 0x0b}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fn := wasm.Func{BodyBytes: tc.body}
			m := &wasm.Module{Types: []wasm.RecType{{SubTypes: []wasm.SubType{{Final: true, Comp: *numeric}}}, {SubTypes: []wasm.SubType{{Final: true, Comp: *private}}}}, FuncTypes: []wasm.TypeIdx{{Index: 1}}, Code: []wasm.Func{fn}}
			frontendErr := wasm.ValidateModuleWithWorkers(m, 1)
			if (frontendErr == nil) != tc.valid {
				t.Fatal("frontend oracle", frontendErr, tc.valid)
			}
			_, swarErr := walkSWARPairsWithReferenceFunctionMode(&fn, private, memoryProfile{}, []*wasm.CompType{numeric, private}, []*wasm.CompType{private}, nil, 0, nil, nil, 0, 0, true, false, false, false, false, false, true, true, nil)
			if (swarErr == nil) != (frontendErr == nil) {
				t.Fatal("SWAR call_ref floor disagrees with frontend", swarErr, frontendErr)
			}
			_, err := walkInteger(&fn, private, state, nil)
			if (err == nil) != tc.valid {
				t.Fatal("call_ref current-floor carrier proof", err, tc.valid)
			}
		})
	}
}

func referenceNumericCallWitnessModule(call bool) []byte {
	body := []byte{0x20, 0, 0x0b}
	if call {
		body = []byte{0x20, 0, 0xd2, 0, 0x14, 0, 0x0b}
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}))), wasmtest.Section(3, []byte{3, 0, 0, 1}), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code(body), wasmtest.Code([]byte{0x20, 0, 0x0b}))))
}
func TestReferenceFunctionNumericCallsStagedWitness(t *testing.T) {
	for _, call := range []bool{false, true} {
		t.Run(fmt.Sprintf("call%t", call), func(t *testing.T) {
			m, err := wasm.DecodeModule(referenceNumericCallWitnessModule(call))
			if err != nil {
				t.Fatal(err)
			}
			c, err := closedReferenceCompile(m, false, false)
			if err != nil {
				t.Fatal(err)
			}
			o := closedReferenceProductionOptions(false, true)
			o.referenceFunctions = true
			o.referenceCallInstructions = call
			if image, err := stageClosedReferenceProductionModule(c, o, m, false); image == nil || err != nil {
				t.Fatal("exact call-only witness", err)
			}
			o.referenceCallInstructions = !call
			pressure := 0
			o.MemoryPressure = func() { pressure++ }
			if image, err := stageClosedReferenceProductionModule(c, o, m, false); image != nil || !errors.Is(err, ErrUnsupported) || pressure != 0 {
				t.Fatal("missing/spurious call witness", err, pressure)
			}
		})
	}
}
func TestReferenceFunctionNumericCallsDeadCountBoundary(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, n := range []int{maxDirectCalls, maxDirectCalls + 1} {
			t.Run(fmt.Sprintf("vector%t-count%d", vector, n), func(t *testing.T) {
				m := referenceNumericCallsDecode(t, vector, false, false, 0, 0)
				body := []byte{0}
				for range n {
					body = append(body, 0xd0, 1, 0x14, 1)
				}
				body = append(body, 0x0b)
				m.Code[0].BodyBytes = body
				o := closedReferenceProductionOptions(false, true)
				o.ImportBindings = hostImportBindings(3)
				pressure := 0
				o.MemoryPressure = func() { pressure++ }
				image, err := CompileModuleWith(m, o)
				if n == maxDirectCalls {
					if image == nil || err != nil || pressure != 1 {
						t.Fatal("exact dead site bound", err, pressure)
					}
				} else if image != nil || err == nil || pressure != 0 {
					t.Fatal("dead site bound plus one", err, pressure)
				}
			})
		}
	}
}
