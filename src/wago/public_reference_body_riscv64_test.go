//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Exports declare ref.func targets without an artificial table/global/element.
func rv64BodyReferenceModule(vector, imported bool) []byte {
	typ, last := wasm.I64, uint32(255)
	constant := []byte{0x42, 0x7f}
	if vector {
		typ, last = wasm.V128, 127
		constant = append([]byte{0xfd, 12}, make([]byte, 16)...)
		for i := 2; i < len(constant); i++ {
			constant[i] = byte(i * 17)
		}
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
	))}
	funcs := []byte{3, 0, 1, 2}
	if imported {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(
			append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0))))
		funcs = []byte{2, 1, 2}
	}
	body := append([]byte{1}, wasmtest.ULEB(last)...)
	body = append(body, 0x70)
	// Fill the bounded local frame with non-null descriptors, then exercise
	// local homes, a reference join, typed select and preservation across calls.
	for i := uint32(1); i <= last; i++ {
		body = append(body, 0xd2, 0, 0x21)
		body = append(body, wasmtest.ULEB(i)...)
	}
	body = append(body, 0x20, 0, 0x04, 0x70, 0x20)
	body = append(body, wasmtest.ULEB(last)...)
	body = append(body, 0x05, 0xd0, 0x70, 0x0b, 0xd0, 0x70, 0x20, 0, 0x1c, 1, 0x70)
	// Keep the selected reference on the operand stack across the numeric call.
	body = append(body, constant...)
	body = append(body, 0x10, 0, 0x1a, 0xd1, 0x20)
	body = append(body, wasmtest.ULEB(last)...)
	body = append(body, 0xd1, 0x72, 0x0b)
	bodies := [][]byte{append(wasmtest.ULEB(uint32(len(body))), body...), wasmtest.Code([]byte{0xd2, 0, 0xd1, 0x0b})}
	if !imported {
		bodies = append([][]byte{wasmtest.Code([]byte{0x20, 0, 0x0b})}, bodies...)
	}
	return wasmtest.Module(append(sections,
		wasmtest.Section(3, funcs),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("target", 0, 0), wasmtest.ExportEntry("carrier", 0, 1), wasmtest.ExportEntry("leaf", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(bodies...)),
	)...)
}

func TestRV64PublicBodyReferencesWithoutState(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, imported := range []bool{false, true} {
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-imported%t-artifact%t", vector, imported, artifact), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					bindings := NewImports()
					var producer *Instance
					var pc *Compiled
					if imported {
						typ := wasm.I64
						if vector {
							typ = wasm.V128
						}
						bare := rv64PublicNativeIdentityModule([]wasm.ValType{typ})
						pc = rv64TableStageCompile(t, fmt.Sprintf("ref-body-vector%t-bare", vector), bare, artifact)
						producer = rv64NativeTokenInstantiate(t, rt, pc, NewImports())
						if pc.needsFuncRefDescs() {
							t.Fatal("bare producer requires descriptors")
						}
						defer producer.Close()
						target, err := producer.ExportedFunc("identity")
						if err != nil {
							t.Fatal(err)
						}
						bindings.Function("env", "target", target)
					}
					c := rv64TableStageCompile(t, fmt.Sprintf("ref-body-vector%t-imported%t", vector, imported), rv64BodyReferenceModule(vector, imported), artifact)
					in := rv64NativeTokenInstantiate(t, rt, c, bindings)
					defer in.Close()
					if !c.NeedsFuncRefDescs || len(in.funcRefDescs) != (c.NumImports+len(c.Funcs)+1)*40 || in.tableDescPtr != 0 || len(in.globalCells) != 0 {
						t.Fatal("descriptor-only storage contract")
					}
					if producer != nil {
						producer.Close()
						pc.Close()
					}
					c.Close() // Instance ownership must retain its code and descriptors.
					fn, err := in.WasmFunc("carrier")
					if err != nil {
						t.Fatal(err)
					}
					for _, input := range []uint64{0, 1, 0xdeadbeef00000000, ^uint64(0)} {
						want := uint64(0)
						if uint32(input) == 0 {
							want = 1
						}
						importedGlobalInvoke(t, in, "carrier", []uint64{want}, input)
						got, err := fn.Invoke(input)
						if err != nil || !slices.Equal(got, []uint64{want}) {
							t.Fatal("prepared body reference", got, err)
						}
						session, err := fn.OpenSession()
						if err != nil {
							t.Fatal(err)
						}
						got, err = session.Invoke1(input)
						session.Close()
						if err != nil || !slices.Equal(got, []uint64{want}) {
							t.Fatal("session body reference", got, err)
						}
					}
					importedGlobalInvoke(t, in, "leaf", []uint64{0})
					if len(rt.refStore.byToken) != 0 || len(rt.refStore.byIdentity) != 0 {
						t.Fatal("internal body references issued public tokens")
					}
				})
			}
		}
	}
}

func TestRV64PublicBodyReferenceAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source and metadata admission")
	}
	for _, vector := range []bool{false, true} {
		source := rv64BodyReferenceModule(vector, false)
		c := rv64TableStageCompile(t, fmt.Sprintf("ref-body-vector%t-local", vector), source, false)
		for name, mutate := range map[string]func(*Compiled){
			"key-count": func(v *Compiled) { v.FuncTypeID = v.FuncTypeID[:len(v.FuncTypeID)-1] },
			"key":       func(v *Compiled) { v.FuncTypeID = slices.Clone(v.FuncTypeID); v.FuncTypeID[0] ^= 1 },
			"context":   func(v *Compiled) { v.nativeTableContextABI = false },
			"reference": func(v *Compiled) { v.nativeTableReferenceABI = false },
			"frame":     func(v *Compiled) { v.registerABIDisabled = false },
			"feature":   func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
			"table":     func(v *Compiled) { v.TableSize = 1 },
		} {
			copy := *c.executionView()
			mutate(&copy)
			if err := railshotValidateCompiled(&copy); err == nil {
				t.Fatal("invalid descriptor metadata admitted", vector, name)
			}
			if name != "table" { // Absent-table fields are not serialized.
				blob, err := marshalCompiled(&copy)
				if err == nil {
					loaded, err := LoadTrustedArtifact(blob)
					if loaded != nil {
						loaded.Close()
					}
					if err == nil {
						t.Fatal("invalid descriptor artifact admitted", vector, name)
					}
				}
			}
		}
		for name, candidate := range map[string][]byte{
			"undeclared":        rv64ReplaceTableStageSection(t, source, 7, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("carrier", 0, 1)))),
			"dead-out-of-range": rv64ReplaceTableStageSection(t, source, 10, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0xd2, 3, 0x1a, 0x0b}), wasmtest.Code([]byte{0x41, 0, 0x0b}), wasmtest.Code([]byte{0x41, 0, 0x0b})))),
		} {
			v, err := Compile(NewRuntimeConfig(), candidate)
			if v != nil {
				v.Close()
			}
			if v != nil || err == nil {
				t.Fatal("invalid source admitted", name, err)
			}
		}
		v, err := Compile(NewRuntimeConfig().WithFeature(CoreFeatureReferenceTypes, false), source)
		if v != nil {
			v.Close()
		}
		if v != nil || err == nil {
			t.Fatal("disabled feature admitted", err)
		}
	}
}

func TestRV64PublicBodyReferenceMissingDirectoryTraps(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("trusted metadata mutation")
	}
	for _, vector := range []bool{false, true} {
		c := rv64TableStageCompile(t, fmt.Sprintf("ref-body-vector%t-local", vector), rv64BodyReferenceModule(vector, false), false)
		// Trusted artifacts contain executable code; numeric metadata alone cannot
		// reconstruct a body's descriptor use. The existing null-pointer guard
		// must trap safely if both directory and context-header witnesses are
		// removed. A header-only arena cannot validate arbitrary trusted code.
		c.executionView().NeedsFuncRefDescs = false
		c.executionView().needsFuncRefContextHeader = false
		rt := NewRuntime()
		in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
		if len(in.funcRefDescs) != 0 {
			t.Fatal("missing-directory fixture allocated descriptors")
		}
		_, err := in.Invoke("leaf")
		var trap *coreruntime.TrapError
		if !errors.As(err, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds {
			t.Fatal("missing descriptor directory", vector, err)
		}
		in.Close()
		rt.Close()
	}
}

func TestRV64PublicBodyReferenceHostReexports(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "ref-body-vectorfalse-importedtrue", rv64BodyReferenceModule(false, true), artifact)
			rt := NewRuntime()
			defer rt.Close()
			host := rv64HostTokenOwner(t, rt, ValI64, slotHostFunc(func(_ HostModule, p, r []uint64) { copy(r, p) }))
			defer host.Close()
			mod, err := rt.Module(c)
			if err != nil {
				t.Fatal(err)
			}
			for _, binding := range []any{HostCallFunc(func(call HostCall) { copy(call.ResultSlots(), call.ParamSlots()) }), host} {
				in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Function("env", "target", binding)))
				if in == nil || err != nil {
					t.Fatal("host reexport constructor", err)
				}
				for _, input := range []uint64{0, 1, ^uint64(0)} {
					if got, err := in.Invoke("target", input); err != nil || !slices.Equal(got, []uint64{input}) {
						t.Fatal("host reexport identity", got, err)
					}
				}
				for _, input := range []uint64{0, 1} {
					if got, err := in.Invoke("carrier", input); err != nil || !slices.Equal(got, []uint64{1 - input}) {
						t.Fatal("host reference carrier", got, err)
					}
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
			}
			mod.Close()
		})
	}
}
