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

func rv64ReferenceIndexedTypesSource(vector, nullable, imported bool, shape string) []byte {
	typ, padding := wasm.I32, uint32(254)
	if vector {
		typ, padding = wasm.V128, 126
	}
	sig := []wasm.ValType{typ}
	types := make([][]byte, 65)
	for i := range types {
		types[i] = wasmtest.FuncType(nil, nil)
	}
	types[0], types[64] = wasmtest.FuncType(sig, sig), wasmtest.FuncType(sig, sig)
	types[1] = wasmtest.FuncType([]wasm.ValType{typ, wasm.I32}, sig)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...))}
	codes := [][]byte{wasmtest.Code([]byte{0x41, 11, 0x24, 0, 0x20, 0, 0x0b}), wasmtest.Code([]byte{0x41, 22, 0x24, 0, 0x20, 0, 0x0b})}
	indices := []byte{3, 0, 64, 1}
	if imported {
		im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 64)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(im)))
		indices, codes = []byte{2, 64, 1}, codes[1:]
	}
	sections = append(sections, wasmtest.Section(3, indices), wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}), wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2), wasmtest.ExportEntry("path", 3, 0))), wasmtest.Section(9, []byte{1, 3, 0, 2, 0, 1}))
	form := byte(0x64)
	if nullable {
		form = 0x63
	}
	decls := byte(1)
	if shape == "local" {
		padding--
		decls++
	}
	locals := append([]byte{decls}, wasmtest.ULEB(padding)...)
	locals = append(locals, wasm.MustEncodeValType(typ))
	body := []byte{0x41, 0, 0x24, 0}
	if shape == "local" {
		locals = append(locals, 1, form, 0xc0, 0)
		index := wasmtest.ULEB(padding + 2)
		if nullable {
			body = append(body, 0x20)
			body = append(body, index...)
			body = append(body, 0xd1, 0x45, 4, 0x40, 0, 0x0b)
		}
		// Initialize before entering a nested control frame, so the non-null
		// local remains definitely initialized at its later use.
		body = append(body, 0xd2, 1, 0x21)
		body = append(body, index...)
		body = append(body, 0x20, 1, 4, 0x40, 0xd2, 0, 0x21)
		body = append(body, index...)
		body = append(body, 0x0b, 0x41, 0, 0x20)
		body = append(body, index...)
	} else if shape == "block" {
		body = append(body, 0x41, 0, 0x20, 1, 4, form, 0xc0, 0, 0xd2, 0, 5, 0xd2, 1, 0x0b)
	} else {
		body = append(body, 0x41, 0, 0xd2, 0, 0xd2, 1, 0x20, 1, 0x1c, 1, form, 0xc0, 0)
	}
	// The actual indirect call and distinct side effects observe the descriptor
	// selected through the sole indexed annotation; there is no call_ref or
	// other typed instruction that could supply its feature witness.
	body = append(body, 0x26, 0, 0x20, 0, 0x41, 0, 0x11, 64, 0, 0x0b)
	code := append(locals, body...)
	code = append(wasmtest.ULEB(uint32(len(code))), code...)
	codes = append(codes, code)
	return wasmtest.Module(append(sections, wasmtest.Section(10, wasmtest.Vec(codes...)))...)
}

func rv64ReferenceIndexedTypesInvoke(t *testing.T, in *Instance, vector, imported bool) {
	t.Helper()
	args, want := rv64ElementGlobalArguments(vector)
	args = append(args, 0)
	for _, condition := range []uint64{0xdeadbeef00000000, 0xdeadbeef00000001, 0x80000000, 0xffffffff00000000, 1} {
		args[len(args)-1] = condition
		importedGlobalInvoke(t, in, "run", want, args...)
		path := uint64(22)
		if uint32(condition) != 0 {
			path = 11
			if imported {
				path = 0
			}
		}
		if got, err := in.Global("path"); err != nil || got != path {
			t.Fatal("indexed annotation selected descriptor", got, path, err)
		}
	}
	before := slices.Clone(args)
	if a := testing.AllocsPerRun(10, func() {
		got, err := in.Invoke("run", args...)
		if err != nil || !slices.Equal(got, want) || !slices.Equal(args, before) {
			panic("warm indexed reference annotation")
		}
	}); a != 0 {
		t.Fatal("warm indexed annotation allocation", a)
	}
}

func TestRV64PublicReferenceIndexedTypesSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, nullable := range []bool{false, true} {
				for _, shape := range []string{"local", "block", "select"} {
					t.Run(fmt.Sprintf("artifact%t-vector%t-nullable%t-%s", artifact, vector, nullable, shape), func(t *testing.T) {
						c := rv64NativeTailCompile(t, fmt.Sprintf("reference-indexed-types-%t-%t-%s", vector, nullable, shape), rv64ReferenceIndexedTypesSource(vector, nullable, false, shape), artifact)
						if !c.nativeReferenceControlABI || c.nativeReferenceCallABI || !c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) || rv64NativeTailModule(c) != nil {
							t.Fatal("indexed annotation-only witness")
						}
						in, err := Instantiate(c)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						rv64ReferenceIndexedTypesInvoke(t, in, vector, false)
					})
				}
			}
		}
	}
}

func TestRV64PublicReferenceIndexedTypesDeadWitness(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, nullable := range []bool{false, true} {
				for _, shape := range []string{"local", "block", "select"} {
					t.Run(fmt.Sprintf("artifact%t-vector%t-nullable%t-%s", artifact, vector, nullable, shape), func(t *testing.T) {
						form, typ, n := byte(0x64), wasm.I32, 256
						if nullable {
							form = 0x63
						}
						if vector {
							typ, n = wasm.V128, 128
						}
						hidden := make([]wasm.ValType, n)
						for i := range hidden {
							hidden[i] = typ
						}
						types := make([][]byte, 65)
						for i := range types {
							types[i] = wasmtest.FuncType(nil, nil)
						}
						types[64] = wasmtest.FuncType(hidden, hidden)
						locals, body := []byte{0}, []byte{0}
						switch shape {
						case "local":
							locals = []byte{1, 1, form, 0xc0, 0}
						case "block":
							body = append(body, 2, form, 0xc0, 0, 0, 0x0b, 0x1a)
						case "select":
							body = append(body, 0x1c, 1, form, 0xc0, 0, 0x1a)
						}
						pc := uint32(len(locals))
						code := append(locals, append(body, 0x0b)...)
						code = append(wasmtest.ULEB(uint32(len(code))), code...)
						source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(code)))
						c := rv64NativeTailCompile(t, fmt.Sprintf("reference-indexed-types-dead-%t-%t-%s", vector, nullable, shape), source, artifact)
						if !c.nativeReferenceControlABI || c.nativeReferenceCallABI || !c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) || !railshotRequiresNativeOwnerScope(c) || rv64NativeTailModule(c) != nil {
							t.Fatal("dead indexed annotation witness")
						}
						p, r, err := railshotImportedCallSlots(c, 0, 0)
						if err != nil || p != 0 || r != 0 {
							t.Fatal("indexed annotation acquired declared-call transport", p, r, err)
						}
						in, err := Instantiate(c)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						if len(in.funcRefDescs) != 40 {
							t.Fatal("indexed annotation anchor extent", len(in.funcRefDescs))
						}
						for range 3 {
							_, err = in.Invoke("run")
							var trap *coreruntime.TrapError
							if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != pc {
								t.Fatal("dead indexed annotation source trap", err)
							}
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicReferenceIndexedTypesRetiredImportedOwner(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, nullable := range []bool{false, true} {
				for _, shape := range []string{"local", "block", "select"} {
					t.Run(fmt.Sprintf("artifact%t-vector%t-nullable%t-%s", artifact, vector, nullable, shape), func(t *testing.T) {
						rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
						t.Cleanup(func() { rv64HostCertificateCloseRuntime(t, rt) })
						typ := wasm.I32
						if vector {
							typ = wasm.V128
						}
						pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-indexed-owner-%t", vector), rv64PublicNativeIdentityModule([]wasm.ValType{typ}), artifact)
						producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
						fn, err := producer.ExportedFunc("identity")
						if err != nil {
							t.Fatal(err)
						}
						c := rv64NativeTailCompile(t, fmt.Sprintf("reference-indexed-borrower-%t-%t-%s", vector, nullable, shape), rv64ReferenceIndexedTypesSource(vector, nullable, true, shape), artifact)
						in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Function("env", "target", fn))
						producer.Close()
						pc.Close()
						c.Close()
						runtime.GC()
						if !producer.hasPhysicalResources() {
							t.Fatal("indexed reference owner retired before execution")
						}
						rv64ReferenceIndexedTypesInvoke(t, in, vector, true)
						state := &in.pluginState.Load().railshotInvocation
						if state.tableTargets[0] == nil || state.tableDepth != 0 || state.tailDispatcher != nil {
							t.Fatal("indexed reference retained scope")
						}
						if err := in.Close(); err != nil {
							t.Fatal(err)
						}
						rv64HostCertificateCloseRuntime(t, rt)
						if producer.hasPhysicalResources() || in.hasPhysicalResources() || pc.codeCache.mem != nil || c.codeCache.mem != nil {
							t.Fatal("indexed reference final retirement")
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicReferenceIndexedTypesAdmission(t *testing.T) {
	for _, nullable := range []bool{false, true} {
		for _, shape := range []string{"local", "block", "select"} {
			t.Run(fmt.Sprintf("nullable%t-%s", nullable, shape), func(t *testing.T) {
				source := rv64ReferenceIndexedTypesSource(false, nullable, false, shape)
				if c, err := compatibilityDefaultConfig().Compile(source); c != nil || err == nil {
					if c != nil {
						c.Close()
					}
					t.Fatal("indexed annotation bypassed public typed feature gate")
				}
				c := rv64NativeTailCompile(t, "reference-indexed-marker", source, false)
				candidate := rv64ReferenceStartCopy(c.executionView())
				candidate.nativeReferenceControlABI = false
				if err := candidate.validate(); err == nil {
					t.Fatal("indexed annotation missing witness admitted")
				}
				blob, err := marshalCompiled(candidate)
				if err == nil {
					if got, err := LoadTrustedArtifact(blob); err == nil {
						if got != nil {
							got.Close()
						}
						t.Fatal("indexed annotation artifact missing witness admitted")
					}
				}
			})
		}
	}
}
