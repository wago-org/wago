//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Local-only and block-only variants have no d4/d5/d6 opcode: the non-null type
// itself must cause feature admission and the retained-carrier witness.
func rv64ReferenceTypesSource(vector, nullable, local, imported bool) []byte {
	typ, padding := wasm.I32, uint32(255)
	if vector {
		typ, padding = wasm.V128, 127
	}
	sig := []wasm.ValType{typ}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(sig, sig)))}
	if imported {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, false))))
	}
	sections = append(sections, wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))))
	if !imported {
		sections = append(sections, wasmtest.Section(9, []byte{1, 3, 0, 1, 0}))
	}
	form := byte(0x64)
	if nullable {
		form = 0x63
	}
	ref := []byte{0xd2, 0}
	if imported {
		ref = []byte{0x23, 0, 0xd4}
	}
	body := []byte{0x20, 0}
	runs := byte(1)
	if local {
		padding--
		runs++
	}
	locals := append([]byte{runs}, wasmtest.ULEB(padding)...)
	locals = append(locals, wasm.MustEncodeValType(typ))
	if local {
		locals = append(locals, 1, form, 0x70)
		body = append(body, ref...)
		body = append(body, 0x22)
		body = append(body, wasmtest.ULEB(padding+1)...)
		body = append(body, 0x1a, 0x20)
		body = append(body, wasmtest.ULEB(padding+1)...)
	} else {
		body = append(body, 2, form, 0x70)
		body = append(body, ref...)
		body = append(body, 0x0c, 0, 0x0b)
	}
	body = append(body, 0xd1, 4, 0x40, 0, 0x0b, 0x0b) // an unexpectedly null carrier must be observable
	code := append(locals, body...)
	code = append(wasmtest.ULEB(uint32(len(code))), code...)
	return wasmtest.Module(append(sections, wasmtest.Section(10, wasmtest.Vec(code)))...)
}

func rv64ReferenceTypesInvoke(t *testing.T, in *Instance, vector bool) {
	t.Helper()
	args, want := rv64ElementGlobalArguments(vector)
	for range 3 {
		importedGlobalInvoke(t, in, "run", want, args...)
	}
	if a := testing.AllocsPerRun(10, func() {
		got, err := in.Invoke("run", args...)
		if err != nil || !slices.Equal(got, want) {
			panic("warm explicit reference type")
		}
	}); a != 0 {
		t.Fatal("warm type allocation", a)
	}
}

func TestRV64PublicReferenceTypesSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, nullable := range []bool{false, true} {
				for _, local := range []bool{false, true} {
					t.Run(fmt.Sprintf("artifact%t-vector%t-nullable%t-local%t", artifact, vector, nullable, local), func(t *testing.T) {
						source := rv64ReferenceTypesSource(vector, nullable, local, false)
						c := rv64NativeTailCompile(t, fmt.Sprintf("reference-types-%t-%t-%t", vector, nullable, local), source, artifact)
						if c.nativeReferenceControlABI == nullable || c.nativeReferenceCallABI || c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) == nullable || rv64NativeTailModule(c) != nil {
							t.Fatal("type-only feature/witness")
						}
						in, err := Instantiate(c)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						rv64ReferenceTypesInvoke(t, in, vector)
					})
				}
			}
		}
	}
}

func TestRV64PublicReferenceTypesNullableBaseline(t *testing.T) {
	for _, local := range []bool{false, true} {
		source := rv64ReferenceTypesSource(false, true, local, false)
		c, err := compatibilityDefaultConfig().Compile(source)
		if err != nil {
			t.Fatal("equivalent nullable syntax rejected", local, err)
		}
		defer c.Close()
		if c.nativeReferenceControlABI || c.nativeReferenceCallABI || c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) {
			t.Fatal("nullable spelling acquired typed features")
		}
		in, err := Instantiate(c)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		rv64ReferenceTypesInvoke(t, in, false)
	}
}

func TestRV64PublicReferenceTypesDeadWitness(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, local := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-local%t", artifact, local), func(t *testing.T) {
				body := []byte{2, 0x64, 0x70, 0, 0x0b, 0x1a, 0x0b}
				locals := []byte{0}
				pc := uint32(4)
				if local {
					body = []byte{0, 0x0b}
					locals = []byte{1, 1, 0x64, 0x70}
					pc = 4
				}
				code := append(locals, body...)
				code = append(wasmtest.ULEB(uint32(len(code))), code...)
				source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(code)))
				c := rv64NativeTailCompile(t, fmt.Sprintf("reference-types-dead-%t", local), source, artifact)
				if !c.nativeReferenceControlABI || c.nativeReferenceCallABI || !c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) || !railshotRequiresNativeOwnerScope(c) {
					t.Fatal("dead type witness")
				}
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if len(in.funcRefDescs) != 40 {
					t.Fatal("type-only descriptor extent", len(in.funcRefDescs))
				}
				for range 3 {
					_, err = in.Invoke("run")
					var trap *coreruntime.TrapError
					if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != pc {
						t.Fatal("dead explicit type trap", err)
					}
				}
			})
		}
	}
}

func TestRV64PublicReferenceTypesRetiredGlobalOwner(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, local := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-local%t", artifact, vector, local), func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					t.Cleanup(func() { rv64HostCertificateCloseRuntime(t, rt) })
					producerSource := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd2, 0, 0x0b}))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("reference", 3, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}))))
					pc := rv64NativeTailCompile(t, "reference-types-owner", producerSource, artifact)
					producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
					g, err := producer.ExportedGlobalObject("reference")
					if err != nil {
						t.Fatal(err)
					}
					c := rv64NativeTailCompile(t, fmt.Sprintf("reference-types-borrower-%t-%t", vector, local), rv64ReferenceTypesSource(vector, false, local, true), artifact)
					in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Global("env", "ref", g))
					producer.Close()
					pc.Close()
					c.Close()
					runtime.GC()
					if !producer.hasPhysicalResources() {
						t.Fatal("type carrier owner retired early")
					}
					rv64ReferenceTypesInvoke(t, in, vector)
					state := &in.pluginState.Load().railshotInvocation
					if state.tableTargets[0] == nil || state.tableDepth != 0 || state.tailDispatcher != nil {
						t.Fatal("type carrier retained scope")
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
					rv64HostCertificateCloseRuntime(t, rt)
					if producer.hasPhysicalResources() || in.hasPhysicalResources() || pc.codeCache.mem != nil || c.codeCache.mem != nil {
						t.Fatal("type carrier final retirement")
					}
				})
			}
		}
	}
}
