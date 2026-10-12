//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceNumericMaximumValue(vector bool, i int) []uint64 {
	if vector {
		return []uint64{0x123456789abcdef0 + uint64(i)*37, 0x7ff0000000000001 + uint64(i)*53}
	}
	return []uint64{uint64(1 + i*37)}
}

func rv64ReferenceNumericCallsMaximumModule(vector, imported, call bool) ([]byte, []wasm.ValType) {
	return rv64ReferenceNumericMaximumModule(vector, imported, call, false)
}
func rv64ReferenceNumericMaximumModule(vector, imported, call, ownedTable bool) ([]byte, []wasm.ValType) {
	return rv64ReferenceMaximumModule(vector, imported, call, ownedTable, false)
}
func rv64ReferenceMaximumModule(vector, imported, call, ownedTable, properTail bool, indirect ...bool) ([]byte, []wasm.ValType) {
	n, typ, stride := 256, wasm.I32, 4
	if vector {
		n, typ, stride = 128, wasm.V128, 16
	} else if imported {
		n = coreruntime.MaxHostArity
	}
	types := make([]wasm.ValType, n)
	var identity, body []byte
	body = append(body, 0x20, 0) // unrelated lower private reference
	for i := range types {
		types[i] = typ
		identity = append(identity, 0x20)
		identity = append(identity, wasmtest.ULEB(uint32(i))...)
		value := rv64ReferenceNumericMaximumValue(vector, i)
		if vector {
			body = append(body, 0xfd, 12)
			body = binary.LittleEndian.AppendUint64(body, value[0])
			body = binary.LittleEndian.AppendUint64(body, value[1])
		} else {
			body = append(body, 0x41)
			body = append(body, wasmtest.SLEB32(int32(value[0]))...)
		}
	}
	identity = append(identity, 0x0b)
	var tailBody []byte
	if properTail {
		tailBody = append(slices.Clone(body[2:]), 0xd2, 0, 0x15, 0, 0x0b)
		if len(indirect) != 0 && indirect[0] {
			tailBody = append(slices.Clone(body[2:]), 0x41, 0, 0x13, 0, 0, 0x0b)
		}
		// A distinct enclosing private frame keeps both a lower reference and
		// its local alive while the private tail source discards its parameter.
		body = []byte{0x20, 0, 0x20, 0, 0x10, 3}
	} else if call && ownedTable {
		body = append(body, 0x41, 0, 0x11, 0, 0)
	} else if call {
		body = append(body, 0xd2, 0, 0x14, 0)
	} else {
		body = append(body, 0x10, 0)
	}
	// Persist every returned position separately; sum/XOR would hide reordering.
	for i := n - 1; i >= 0; i-- {
		body = append(body, 0x21, 1, 0x41)
		body = append(body, wasmtest.SLEB32(int32(i*stride))...)
		body = append(body, 0x20, 1)
		if vector {
			body = append(body, 0xfd, 11, 4, 0)
		} else {
			body = append(body, 0x36, 2, 0)
		}
	}
	body = append(body, 0x21, 0, 0x41)
	body = append(body, wasmtest.SLEB32(int32((n-1)*stride))...)
	if vector {
		body = append(body, 0xfd, 0, 4, 0)
	} else {
		body = append(body, 0x28, 2, 0)
	}
	body = append(body, 0x20, 0, 0x0b)
	privateBody := append([]byte{1, 1, wasm.MustEncodeValType(typ)}, body...)
	privateBody = append(wasmtest.ULEB(uint32(len(privateBody))), privateBody...)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, []wasm.ValType{typ}), wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{typ, wasm.FuncRef})))}
	if imported && !vector {
		declared := make([]wasm.ValType, 256)
		for i := range declared {
			declared[i] = wasm.I32
		}
		sections[0] = wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, []wasm.ValType{typ}), wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{typ, wasm.FuncRef}), wasmtest.FuncType(declared, declared)))
	}
	tailType := byte(3)
	if imported && !vector {
		tailType = 4
	}
	if properTail {
		var entries [][]byte
		entries = append(entries, wasmtest.FuncType(types, types), wasmtest.FuncType(nil, []wasm.ValType{typ}), wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{typ, wasm.FuncRef}))
		if imported && !vector {
			declared := make([]wasm.ValType, 256)
			for i := range declared {
				declared[i] = wasm.I32
			}
			entries = append(entries, wasmtest.FuncType(declared, declared))
		}
		entries = append(entries, wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, types))
		sections[0] = wasmtest.Section(1, wasmtest.Vec(entries...))
	}
	funcs := []byte{3, 0, 1, 2}
	codes := [][]byte{wasmtest.Code(identity)}
	if imported {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("identity")...), 0, 0))))
		funcs = []byte{2, 1, 2}
		codes = nil
	}
	codes = append(codes, wasmtest.Code([]byte{0xd2, 1, 0x10, 2, 0x24, 0, 0x0b}), privateBody)
	if properTail {
		funcs[0]++
		funcs = append(funcs, tailType)
		codes = append(codes, wasmtest.Code(tailBody))
	}
	sections = append(sections, wasmtest.Section(3, funcs))
	if ownedTable {
		sections = append(sections, wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}))
	}
	elements := []byte{1, 3, 0, 2, 0, 1}
	if ownedTable {
		elements = []byte{2, 0, 0x41, 0, 0x0b, 1, 0, 3, 0, 2, 0, 1}
	}
	sections = append(sections, wasmtest.Section(5, []byte{1, 1, 1, 1}), wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b}))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(9, elements), wasmtest.Section(10, wasmtest.Vec(codes...)))
	return wasmtest.Module(sections...), types
}

func TestRV64PublicReferenceFunctionNumericCallsMaximum(t *testing.T) {
	rv64ReferenceFunctionNumericCallsMaximum(t, false)
}
func TestRV64PublicReferenceFunctionOwnedTableMaximum(t *testing.T) {
	rv64ReferenceFunctionNumericCallsMaximum(t, true)
}
func TestRV64PublicReferenceFunctionProperReferenceTailMaximum(t *testing.T) {
	rv64ReferenceFunctionNumericCallsMaximum(t, false, true)
}
func TestRV64PublicReferenceFunctionProperIndirectTailMaximum(t *testing.T) {
	rv64ReferenceFunctionNumericCallsMaximum(t, true, true, true)
}
func rv64ReferenceFunctionNumericCallsMaximum(t *testing.T, ownedTable bool, proper ...bool) {
	properTail := len(proper) != 0 && proper[0]
	indirectTail := len(proper) > 1 && proper[1]
	t.Helper()
	product := "numeric-call"
	if ownedTable {
		product = "owned-table"
	}
	if properTail {
		product = "proper-reference-tail"
	}
	if indirectTail {
		product = "proper-indirect-tail"
	}
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, imported := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-imported%t", artifact, vector, imported), func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rv64HostCertificateCloseRuntime(t, rt)
					source, types := rv64ReferenceMaximumModule(vector, imported, true, ownedTable, properTail, indirectTail)
					c := rv64NativeTailCompile(t, fmt.Sprintf("reference-%s-max-v%t-i%t", product, vector, imported), source, artifact)
					im := NewImports()
					var native *Instance
					var nc *Compiled
					if imported {
						nc = rv64NativeTailCompile(t, fmt.Sprintf("reference-numeric-call-max-native-v%t", vector), rv64PublicNativeIdentityModule(types), artifact)
						native = rv64NativeTokenInstantiate(t, rt, nc, NewImports())
						f, err := native.ExportedFunc("identity")
						if err != nil {
							t.Fatal(err)
						}
						im.Function("env", "identity", f)
					}
					in := rv64NativeTokenInstantiate(t, rt, c, im)
					p, r, err := railshotImportedCallSlots(c, 0, 1)
					wantParams, wantResults := 256, 256
					if properTail && !indirectTail && !imported {
						wantParams, wantResults = 0, 1
					}
					if properTail && !indirectTail && imported && !vector {
						wantParams, wantResults = coreruntime.MaxHostArity, coreruntime.MaxHostArity
					}
					if err != nil || p != wantParams || r != wantResults {
						t.Fatal("numeric maximum vs skipped private signature", p, r, err)
					}
					rows := 4
					if properTail {
						rows++
					}
					if len(in.funcRefDescs) != rows*40 || !railshotNonCallableFunction(c, 2) || (properTail && !railshotNonCallableFunction(c, 3)) {
						t.Fatal("max private sparse row")
					}
					n, stride := 256, 4
					if vector {
						n, stride = 128, 16
					} else if imported {
						n = coreruntime.MaxHostArity
					}
					want := rv64ReferenceNumericMaximumValue(vector, n-1)
					fn, err := in.WasmFunc("run")
					if err != nil {
						t.Fatal(err)
					}
					session, err := fn.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					defer session.Close()
					if native != nil {
						native.Close()
						nc.Close()
					}
					c.Close()
					for range 3 {
						if got, err := session.Invoke(); err != nil || !slices.Equal(got, want) {
							t.Fatal("max private numeric call_ref transport", got, want, err)
						}
						for i := 0; i < n; i++ {
							memory := in.mem()
							got := []uint64{uint64(binary.LittleEndian.Uint32(memory[i*stride:]))}
							if vector {
								got = []uint64{binary.LittleEndian.Uint64(memory[i*stride:]), binary.LittleEndian.Uint64(memory[i*stride+8:])}
							}
							if expected := rv64ReferenceNumericMaximumValue(vector, i); !slices.Equal(got, expected) {
								t.Fatal("ordered maximum returned slot", i, got, expected)
							}
						}
						value := readGlobalObject(in.globalCells[0], ValFuncRef)
						if index, ok := in.funcrefDescriptorIndex(value); !ok || index != 1 || value>>32 == 0 {
							t.Fatal("maximum transport clobbered raw lower reference", value, index, ok)
						}
					}
					if state := &in.pluginState.Load().railshotInvocation; state.tailDispatcher != nil || state.tailGenerationBudget.count != 0 || state.tableDepth != 0 || rv64EntryCapability(in) != 0 {
						t.Fatal("ordinary call_ref created or retained tail generation")
					}
					session.Close()
					if !artifact && !properTail { // Compare matching private frames/owners, changing only the call form.
						controlSource, _ := rv64ReferenceNumericMaximumModule(vector, imported, false, ownedTable)
						controlCode := rv64NativeTailCompile(t, "reference-"+product+"-max-control", controlSource, false)
						// The logically closed native remains callable through the already attached
						// export used above, but new public attachment is correctly disallowed.
						if !imported {
							control := rv64NativeTokenInstantiate(t, rt, controlCode, NewImports())
							check := func(instance *Instance) {
								if got, err := instance.Invoke("run"); err != nil || !slices.Equal(got, want) {
									panic("warm numeric maximum continuation")
								}
							}
							directAllocs := testing.AllocsPerRun(5, func() { check(control) })
							referenceAllocs := testing.AllocsPerRun(5, func() { check(in) })
							if referenceAllocs != directAllocs || referenceAllocs != 0 {
								t.Fatal("warm own numeric call_ref allocation", referenceAllocs, directAllocs)
							}
						}
					}
					session.Close()
					in.Close()
					if native != nil {
						rv64ReferenceImportedStartRetired(t, native, nc)
					}
				})
			}
		}
	}
}

func rv64ReferenceNumericCallsTrapModule(vector bool) []byte {
	payload := wasm.I32
	if vector {
		payload = wasm.V128
	}
	types := [][]byte{wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), []byte{0x60, 2, 0x63, 0, 0x7f, 1, 0x7f}, wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{payload}, []wasm.ValType{payload})}
	// wasmtest.FuncType is one-byte only; this exact indexed signature is explicit.
	// Choice0 null trap, choice1 recursive stack trap, other choice normal return.
	run := []byte{0x20, 0, 0x45, 0x04, 0x40, 0xd0, 0, 0x41, 0, 0x10, 1, 0x0f, 0x0b, 0xd2, 4, 0x20, 0, 0x10, 1, 0x0b}
	private := []byte{0x20, 1, 0x41, 1, 0x46, 0x04, 0x7f, 0x41, 1, 0xd2, 2, 0x14, 0, 0x05, 0x20, 1, 0x20, 0, 0x14, 0, 0x0b, 0x0b}
	recur := []byte{0x20, 0, 0xd2, 2, 0x14, 0, 0x0b}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, []byte{5, 0, 1, 0, 3, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(9, []byte{1, 3, 0, 3, 0, 2, 4}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(run), wasmtest.Code(private), wasmtest.Code(recur), wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x0b}))))
}

func TestRV64PublicReferenceFunctionNumericCallsTrapReuse(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithNativeStackBytes(MinNativeStackBytes)))
				defer rv64HostCertificateCloseRuntime(t, rt)
				c := rv64NativeTailCompile(t, fmt.Sprintf("reference-numeric-call-traps-v%t", vector), rv64ReferenceNumericCallsTrapModule(vector), artifact)
				in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
				for _, choice := range []uint64{0, 1, 0, 1} {
					_, err := in.Invoke("run", 0xdeadbeef00000000|choice)
					var trap *coreruntime.TrapError
					want := coreruntime.TrapNullReference
					if choice == 1 {
						want = coreruntime.TrapStackFenceBreached
					}
					if !errors.As(err, &trap) || trap.Code != want {
						t.Fatal("numeric private call_ref trap", choice, err)
					}
					if choice == 0 && (len(trap.Frames) == 0 || trap.Frames[0].FunctionIndex != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 19) {
						t.Fatal("private call_ref null source attribution", trap.Frames)
					}
					if got, err := in.Invoke("run", 0xdeadbeef00000002); err != nil || !slices.Equal(got, []uint64{2}) {
						t.Fatal("private numeric call_ref reuse after trap", got, err)
					}
					if state := &in.pluginState.Load().railshotInvocation; state.tableDepth != 0 || state.tailGenerationBudget.count != 0 || rv64EntryCapability(in) != 0 {
						t.Fatal("trap retained call activation")
					}
				}
			})
		}
	}
}
