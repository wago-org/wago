//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Both paths carry numeric payloads through deep control. The exported numeric
// witness distinguishes taking a branch from producing the same final value by
// fallthrough. No reference enters or leaves the external function signature.
func rv64ReferenceBranchSource(vector, nonNull, imported bool) []byte {
	typ, pad := wasm.I64, uint32(254)
	if vector {
		typ, pad = wasm.V128, 126
	}
	sig := []wasm.ValType{typ, wasm.I32}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(sig, sig)))}
	if imported {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, false))))
	}
	sections = append(sections, wasmtest.Section(3, []byte{1, 0}))
	globals := [][]byte{}
	if !imported {
		globals = append(globals, wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd2, 0, 0x0b}))
	}
	globals = append(globals, wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}))
	sections = append(sections, wasmtest.Section(6, wasmtest.Vec(globals...)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("path", 3, 1))))
	body := []byte{0x41, 0, 0x24, 1}
	depth := 127 // the reference-selecting if occupies the final control level
	if nonNull {
		body = append(body, 0x20, 0, 0x20, 1, 2, 0x70)
		depth--
	}
	body = append(body, bytes.Repeat([]byte{2, 0x40}, depth)...)
	body = append(body, 0x42, 1, 0x42, 2)
	if !nonNull {
		body = append(body, 0x20, 0, 0x20, 1)
	}
	body = append(body, 0x20, 1, 4, 0x70, 0x23, 0, 5, 0xd0, 0x70, 0x0b)
	op := byte(0xd5)
	if nonNull {
		op = 0xd6
	}
	body = append(body, op)
	body = append(body, wasmtest.ULEB(uint32(depth))...)
	if nonNull {
		body = append(body, 0x1a, 0x1a, 0x41, 1, 0x24, 1, 0xd0, 0x70, 0x0c)
		body = append(body, wasmtest.ULEB(uint32(depth))...)
	} else {
		body = append(body, 0x1a, 0x41, 1, 0x24, 1, 0x41, 1, 0x6a, 0x0f)
	}
	body = append(body, bytes.Repeat([]byte{0x0b}, depth)...)
	body = append(body, 0x00)
	if nonNull {
		body = append(body, 0x0b, 0xd1, 0x6a)
	}
	body = append(body, 0x0b)
	locals := append([]byte{1}, wasmtest.ULEB(pad)...)
	locals = append(locals, wasm.MustEncodeValType(typ))
	code := append(locals, body...)
	code = append(wasmtest.ULEB(uint32(len(code))), code...)
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(code)))
	return wasmtest.Module(sections...)
}

func rv64ReferenceBranchInvoke(t *testing.T, in *Instance, vector, nonNull bool) {
	t.Helper()
	args := []uint64{0xfedcba9876543210}
	if vector {
		args = append(args, 0x0123456789abcdef)
	}
	args = append(args, 0)
	want := slices.Clone(args)
	for _, raw := range []uint64{0xcafebabe00000000, 0xcafebabe80000001, 0, 1, 0xffffffff} {
		args[len(args)-1] = raw
		want[len(want)-1] = uint64(uint32(raw))
		path := uint64(0)
		if (uint32(raw) != 0) != nonNull {
			want[len(want)-1] = uint64(uint32(raw) + 1)
			path = 1
		}
		importedGlobalInvoke(t, in, "run", want, args...)
		if got, err := in.Global("path"); err != nil || got != path {
			t.Fatal("taken/fallthrough side effect", got, path, err)
		}
	}
	if a := testing.AllocsPerRun(10, func() {
		got, err := in.Invoke("run", args...)
		if err != nil || !slices.Equal(got, want) {
			panic("warm reference branch")
		}
	}); a != 0 {
		t.Fatal("warm reference branch allocation", a)
	}
}

func TestRV64PublicReferenceBranchTransfers(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, nonNull := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-nonnull%t", artifact, vector, nonNull), func(t *testing.T) {
					c := rv64NativeTailCompile(t, fmt.Sprintf("branch-transfer-%t-%t", vector, nonNull), rv64ReferenceBranchSource(vector, nonNull, false), artifact)
					if !c.nativeReferenceControlABI || c.nativeReferenceCallABI || rv64NativeTailModule(c) != nil || !railshotRequiresNativeOwnerScope(c) {
						t.Fatal("branch reference contract")
					}
					in, err := Instantiate(c)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					rv64ReferenceBranchInvoke(t, in, vector, nonNull)
				})
			}
		}
	}
}

func rv64ReferenceBranchDeadSource(vector, nonNull bool) []byte {
	typ, n := wasm.I32, 256
	if vector {
		typ, n = wasm.V128, 128
	}
	hidden := make([]wasm.ValType, n)
	for i := range hidden {
		hidden[i] = typ
	}
	body := []byte{0, 0xd5, 0, 0x1a, 0x0b}
	if nonNull {
		body = []byte{2, 0x70, 0, 0xd6, 0, 0xd0, 0x70, 0x0b, 0x1a, 0x0b}
	}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(hidden, hidden))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
}

func TestRV64PublicReferenceBranchDeadWitness(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, nonNull := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-nonnull%t", artifact, vector, nonNull), func(t *testing.T) {
					c := rv64NativeTailCompile(t, fmt.Sprintf("branch-dead-%t-%t", vector, nonNull), rv64ReferenceBranchDeadSource(vector, nonNull), artifact)
					if !c.nativeReferenceControlABI || c.nativeReferenceCallABI || rv64NativeTailModule(c) != nil || !railshotRequiresNativeOwnerScope(c) {
						t.Fatal("dead branch witness")
					}
					p, r, err := railshotImportedCallSlots(c, 0, 0)
					if err != nil || p != 0 || r != 0 {
						t.Fatal("dead branch acquired call transport", p, r, err)
					}
					in, err := Instantiate(c)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					if len(in.funcRefDescs) != 40 {
						t.Fatal("branch-only code acquired indexed descriptors", len(in.funcRefDescs))
					}
					for range 3 {
						_, err = in.Invoke("run")
						var trap *coreruntime.TrapError
						pc := uint32(1)
						if nonNull {
							pc = 3
						}
						if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 0 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != pc {
							t.Fatal("dead branch source trap and reuse", err)
						}
					}
					state := &in.pluginState.Load().railshotInvocation
					if state.tailDispatcher != nil || state.tailGenerationBudget.count != 0 || rv64EntryCapability(in) != 0 {
						t.Fatal("dead branch acquired tail generation")
					}
				})
			}
		}
	}
}

func TestRV64PublicReferenceBranchLoopParameters(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				types := [][]byte{wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})}
				if vector {
					types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))
				}
				body := []byte{0x20, 0, 3, 0, 0x22, 0, 0x20, 0, 0x45, 4, 0x40, 0, 0x0b, 0x41, 1, 0x6b, 0x22, 0, 0x20, 0, 0x45, 4, 0x70, 0xd2, 0, 5, 0xd0, 0x70, 0x0b, 0xd5, 0, 0x1a, 0x0b, 0x0b}
				source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
				c := rv64NativeTailCompile(t, fmt.Sprintf("branch-loop-%t", vector), source, artifact)
				if !c.nativeReferenceControlABI || c.nativeReferenceCallABI {
					t.Fatal("loop witness")
				}
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				for _, raw := range []uint64{0xcafebabe00000005, 1, 20} {
					importedGlobalInvoke(t, in, "run", []uint64{0}, raw)
				}
			})
		}
	}
}

func TestRV64PublicReferenceBranchRetiredGlobalOwner(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, nonNull := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-nonnull%t", artifact, vector, nonNull), func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					t.Cleanup(func() { rv64HostCertificateCloseRuntime(t, rt) })
					producerSource := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd2, 0, 0x0b}))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("reference", 3, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))))
					pc := rv64NativeTailCompile(t, "branch-owner", producerSource, artifact)
					producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
					global, err := producer.ExportedGlobalObject("reference")
					if err != nil {
						t.Fatal(err)
					}
					c := rv64NativeTailCompile(t, fmt.Sprintf("branch-borrower-%t-%t", vector, nonNull), rv64ReferenceBranchSource(vector, nonNull, true), artifact)
					in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Global("env", "ref", global))
					producer.Close()
					pc.Close()
					c.Close()
					runtime.GC()
					if !producer.hasPhysicalResources() {
						t.Fatal("reference owner retired before branch entry")
					}
					rv64ReferenceBranchInvoke(t, in, vector, nonNull)
					state := &in.pluginState.Load().railshotInvocation
					if state.tableTargets[0] == nil || state.tableDepth != 0 || state.tailDispatcher != nil {
						t.Fatal("branch physical scope lifecycle")
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
					rv64HostCertificateCloseRuntime(t, rt)
					if producer.hasPhysicalResources() || in.hasPhysicalResources() || pc.codeCache.mem != nil || c.codeCache.mem != nil {
						t.Fatal("branch owner/code retained after runtime retirement")
					}
				})
			}
		}
	}
}

func TestRV64PublicReferenceBranchMarkerAdmission(t *testing.T) {
	for _, nonNull := range []bool{false, true} {
		source := rv64ReferenceBranchDeadSource(false, nonNull)
		if c, err := compatibilityDefaultConfig().Compile(source); c != nil || err == nil {
			t.Fatal("public typed-reference gate widened")
		}
		base := rv64NativeTailCompile(t, "branch-marker", source, false)
		c := rv64ReferenceImportTailCopy(base)
		if err := c.validate(); err != nil {
			t.Fatal("unchanged branch metadata", err)
		}
		c.nativeReferenceControlABI = false
		if err := c.validate(); err == nil {
			t.Fatal("branch descriptor without control witness admitted")
		}
		blob, err := marshalCompiled(c)
		if err == nil {
			if got, err := LoadTrustedArtifact(blob); err == nil {
				if got != nil {
					got.Close()
				}
				t.Fatal("branch artifact without control witness admitted")
			}
		}
	}
}
