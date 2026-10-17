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

// Every observed payload and descriptor comes from the tuple that arrived over
// the loop/backedge and br_table; reloading original arguments would hide damage.
func rv64ReferenceControlsSource(vector, indexed, nullable, cell, imported bool, edge string) []byte {
	typ, pad := wasm.I32, uint32(252)
	if vector {
		typ, pad = wasm.V128, 124
	}
	hb, form := byte(0x70), byte(0x64)
	if indexed {
		hb = 0
	}
	if nullable {
		form = 0x63
	}
	tb := wasm.MustEncodeValType(typ)
	mixed := []byte{3, tb, 0x7f, form, hb}
	loop := append([]byte{0x60}, mixed...)
	loop = append(loop, mixed...)
	result := append([]byte{0x60, 0}, mixed...)
	types := [][]byte{wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}), wasmtest.FuncType([]wasm.ValType{typ, wasm.I32}, []wasm.ValType{typ, wasm.I32}), loop, result}
	counter, saved := pad+2, pad+3
	body := []byte{0x41, 0, 0x24, 0, 0x41, 0, 0x24, 1, 0x41, 3, 0x21}
	body = append(body, wasmtest.ULEB(counter)...)
	body = append(body, 0x20, 0, 0x20, 1, 0xd2, 0, 0x20, 1, 4, 2, 0x1a, 0xd2, 1, 5, 0x0b, 0x21)
	body = append(body, wasmtest.ULEB(saved)...)
	body = append(body, 0x20)
	body = append(body, wasmtest.ULEB(saved)...)
	body = append(body, 3, 2, 0x23, 1, 0x41, 1, 0x6a, 0x24, 1)
	if edge == "br-nonnull" {
		body = append(body, 0x21)
		body = append(body, wasmtest.ULEB(saved)...)
	}
	body = append(body, 0x20)
	body = append(body, wasmtest.ULEB(counter)...)
	body = append(body, 0x41, 1, 0x6b, 0x22)
	body = append(body, wasmtest.ULEB(counter)...)
	switch edge {
	case "br-if":
		body = append(body, 0x0d, 0)
	case "br-null":
		body = append(body, 4, 0x63, hb, 0xd0, hb, 5, 0xd2, 0, 0x0b, 0xd5, 0, 0x1a)
	case "br-nonnull":
		body = append(body, 4, 0x63, hb, 0x20)
		body = append(body, wasmtest.ULEB(saved)...)
		body = append(body, 5, 0xd0, hb, 0x0b, 0xd6, 0, 0x20)
		body = append(body, wasmtest.ULEB(saved)...)
	}
	body = append(body, 0x0b, 0x21)
	body = append(body, wasmtest.ULEB(saved)...)
	body = append(body, 0x21, 1, 0x21, 0, 2, 3, 2, 3, 0x42, 9, 0x20, 0, 0x20, 1, 0x20)
	body = append(body, wasmtest.ULEB(saved)...)
	body = append(body, 0x20, 1, 0x0e, 1, 0, 1, 0x0b, 0x0b, 0x21)
	body = append(body, wasmtest.ULEB(saved)...)
	body = append(body, 0x21, 1, 0x21, 0)
	if cell {
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(saved)...)
		body = append(body, 0x24, 2, 0x20, 0)
		if indexed {
			body = append(body, 0x20)
			body = append(body, wasmtest.ULEB(saved)...)
			body = append(body, 0x14, 0)
		}
	} else {
		body = append(body, 0x41, 0, 0x20)
		body = append(body, wasmtest.ULEB(saved)...)
		body = append(body, 0x26, 0, 0x20, 0, 0x41, 0, 0x11, 0, 0)
	}
	body = append(body, 0x20, 1, 0x0b)
	locals := append([]byte{3}, wasmtest.ULEB(pad)...)
	locals = append(locals, wasm.MustEncodeValType(typ), 1, 0x7f, 1)
	locals = append(locals, form, hb)
	code := append(locals, body...)
	code = append(wasmtest.ULEB(uint32(len(code))), code...)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...))}
	indices := []byte{3, 0, 0, 1}
	if imported {
		im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(im)))
		indices = []byte{2, 0, 1}
	}
	sections = append(sections, wasmtest.Section(3, indices))
	if !cell {
		sections = append(sections, wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}))
	}
	globals := [][]byte{wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b}), wasmtest.GlobalEntry(wasm.I32, true, []byte{0x41, 0, 0x0b})}
	if cell {
		globals = append(globals, wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b}))
	}
	codes := [][]byte{wasmtest.Code([]byte{0x41, 11, 0x24, 0, 0x20, 0, 0x0b}), wasmtest.Code([]byte{0x41, 22, 0x24, 0, 0x20, 0, 0x0b}), code}
	if imported {
		codes = codes[1:]
	}
	sections = append(sections, wasmtest.Section(6, wasmtest.Vec(globals...)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2), wasmtest.ExportEntry("path", 3, 0), wasmtest.ExportEntry("count", 3, 1))), wasmtest.Section(9, []byte{1, 3, 0, 2, 0, 1}), wasmtest.Section(10, wasmtest.Vec(codes...)))
	return wasmtest.Module(sections...)
}

func TestRV64PublicReferenceControlsSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, indexed := range []bool{false, true} {
				for _, nullable := range []bool{false, true} {
					for _, cell := range []bool{false, true} {
						for _, edge := range []string{"br-if", "br-null", "br-nonnull"} {
							name := fmt.Sprintf("reference-controls-v%t-i%t-n%t-c%t-%s", vector, indexed, nullable, cell, edge)
							t.Run(fmt.Sprintf("artifact%t-%s", artifact, name), func(t *testing.T) {
								c := rv64NativeTailCompile(t, name, rv64ReferenceControlsSource(vector, indexed, nullable, cell, false, edge), artifact)
								witness := indexed || !nullable || edge != "br-if"
								if c.nativeReferenceControlABI != witness || c.nativeReferenceCallABI != (cell && indexed) || rv64NativeTailModule(c) != nil {
									t.Fatal("control/call witnesses")
								}
								in, err := Instantiate(c)
								if err != nil {
									t.Fatal(err)
								}
								defer in.Close()
								rv64ReferenceControlsInvoke(t, in, vector, indexed, cell, false)
							})
						}
					}
				}
			}
		}
	}
}

func rv64UnusedControlSource(vector, table bool, shape string) []byte {
	typ, n := wasm.I32, 256
	if vector {
		typ, n = wasm.V128, 128
	}
	ref := []byte{0x70}
	if shape == "nonnull" {
		ref = []byte{0x64, 0x70}
	} else if shape == "indexed" {
		ref = []byte{0x63, 0}
	}
	list := wasmtest.ULEB(uint32(n))
	for range n {
		list = append(list, ref...)
	}
	control := append([]byte{0x60}, list...)
	control = append(control, list...)
	numeric := make([]wasm.ValType, 17)
	for i := range numeric {
		numeric[i] = typ
	}
	types := [][]byte{wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}), control, wasmtest.FuncType(numeric, numeric)}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(3, []byte{1, 0})}
	if table {
		sections = append(sections, wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}))))
	return wasmtest.Module(sections...)
}

func TestRV64PublicReferenceControlsUnusedSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, table := range []bool{false, true} {
				for _, shape := range []string{"nullable", "nonnull", "indexed"} {
					name := fmt.Sprintf("reference-controls-unused-v%t-t%t-%s", vector, table, shape)
					t.Run(fmt.Sprintf("artifact%t-%s", artifact, name), func(t *testing.T) {
						c := rv64NativeTailCompile(t, name, rv64UnusedControlSource(vector, table, shape), artifact)
						witness := shape != "nullable"
						if c.nativeReferenceControlABI != witness || c.nativeReferenceCallABI || c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) != witness {
							t.Fatal("unused declaration witness")
						}
						wantSlots := 0
						if table {
							wantSlots = 256
							if vector {
								wantSlots = 128
							}
						}
						p, r, err := railshotImportedCallSlots(c, 0, 0)
						if err != nil || p != wantSlots || r != wantSlots {
							t.Fatal("unused numeric transport/control exclusion", p, r, wantSlots, err)
						}
						if a := testing.AllocsPerRun(10, func() {
							_, _, err := railshotImportedCallSlots(c, 0, 0)
							if err != nil {
								panic(err)
							}
						}); a != 0 {
							t.Fatal("type directory sizing allocation", a)
						}
						in, err := Instantiate(c)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						if witness && !table && len(in.funcRefDescs) != 40 {
							t.Fatal("unused control anchor", len(in.funcRefDescs))
						}
						args, want := rv64ElementGlobalArguments(vector)
						importedGlobalInvoke(t, in, "run", want, args...)
						if a := testing.AllocsPerRun(10, func() {
							got, err := in.Invoke("run", args...)
							if err != nil || !slices.Equal(got, want) {
								panic("unused control fixture")
							}
						}); a != 0 {
							t.Fatal("unused control warm allocation", a)
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicReferenceControlsAdmission(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, shape := range []string{"nullable", "nonnull", "indexed"} {
			c, err := compatibilityDefaultConfig().Compile(rv64UnusedControlSource(vector, false, shape))
			if shape == "nullable" {
				if err != nil || c == nil {
					t.Fatal("baseline nullable controls", err)
				}
			} else if err == nil || c != nil {
				t.Fatal("typed controls bypassed feature gate", shape, err)
			}
			if c != nil {
				c.Close()
			}
		}
		c := rv64NativeTailCompile(t, "reference-controls-metadata", rv64UnusedControlSource(vector, true, "indexed"), false)
		for name, mutate := range map[string]func(*Compiled){
			"missing-marker":            func(v *Compiled) { v.nativeReferenceControlABI = false },
			"missing-typed-feature":     func(v *Compiled) { v.requiredFeatures &^= CoreFeatureTypedFunctionReferences },
			"missing-reference-feature": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
			"nonfinal":                  func(v *Compiled) { v.Types[1].Final = false },
			"super":                     func(v *Compiled) { v.Types[1].Supers = []uint32{0} },
			"fields":                    func(v *Compiled) { v.Types[1].Fields = []FieldTypeDescriptor{{}} },
			"array":                     func(v *Compiled) { v.Types[1].Array.Mutable = true },
			"target-fields":             func(v *Compiled) { v.Types[0].Fields = []FieldTypeDescriptor{{}} },
			"target-array":              func(v *Compiled) { v.Types[0].Array.Mutable = true },
			"numeric-ref-metadata":      func(v *Compiled) { v.Types[0].Params[0].Ref.Nullable = true },
			"exact":                     func(v *Compiled) { v.Types[1].Params[0].Ref.Exact = true },
			"bad-heap":                  func(v *Compiled) { v.Types[1].Params[0].Ref.Heap.TypeIndex = 99 },
			"control-heap":              func(v *Compiled) { v.Types[1].Params[0].Ref.Heap.TypeIndex = 1 },
			"heap-union":                func(v *Compiled) { v.Types[1].Params[0].Ref.Heap.Abstract = AbstractHeapFunc },
			"oversize":                  func(v *Compiled) { v.Types[1].Params = append(v.Types[1].Params, v.Types[1].Params[0]) },
			"function-binding":          func(v *Compiled) { v.Funcs[0].HasTypeIndex = true; v.Funcs[0].TypeIndex = 1 },
			"recursive-group":           func(v *Compiled) { v.Types[1].RecGroup = v.Types[0].RecGroup },
		} {
			t.Run(fmt.Sprintf("vector%t-%s", vector, name), func(t *testing.T) {
				copy := *c.executionView()
				copy.Types = slices.Clone(copy.Types)
				copy.Funcs = slices.Clone(copy.Funcs)
				for i := range copy.Types {
					copy.Types[i].Params = slices.Clone(copy.Types[i].Params)
					copy.Types[i].Results = slices.Clone(copy.Types[i].Results)
				}
				mutate(&copy)
				if err := copy.validate(); err == nil {
					t.Fatal("malformed retained control admitted")
				}
				// Serialization recomputes feature bits and omits inactive heap
				// union data. Require their canonical values if loading succeeds.
				blob, err := marshalCompiled(&copy)
				if err == nil {
					loaded, err := LoadTrustedArtifact(blob)
					canonical := false
					if loaded != nil {
						switch name {
						case "missing-typed-feature":
							canonical = loaded.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences)
						case "missing-reference-feature":
							canonical = loaded.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes)
						case "heap-union":
							canonical = loaded.Types[1].Params[0].Ref.Heap.Abstract == 0
						}
						loaded.Close()
					}
					if err == nil && !canonical {
						t.Fatal("malformed control artifact loaded")
					}
				}

			})
		}
	}
}

func rv64ReferenceControlsInvoke(t *testing.T, in *Instance, vector, indexed, cell, imported bool) {
	t.Helper()
	args, payload := rv64ElementGlobalArguments(vector)
	args = append(args, 0)
	want := append(slices.Clone(payload), 0)
	for _, condition := range []uint64{0xdeadbeef00000000, 0xdeadbeef00000001, 0x80000000, 0xffffffff00000000, 1} {
		args[len(args)-1] = condition
		want[len(want)-1] = uint64(uint32(condition))
		importedGlobalInvoke(t, in, "run", want, args...)
		path := uint64(11)
		if imported {
			path = 0
		}
		if uint32(condition) != 0 {
			path = 22
		}
		if cell {
			descriptor := readGlobalObject(in.globalCells[2], ValFuncRef)
			wantIndex := 0
			if uint32(condition) != 0 {
				wantIndex = 1
			}
			if index, ok := in.funcrefDescriptorIndex(descriptor); !ok || index != wantIndex || descriptor>>32 == 0 {
				t.Fatal("arriving full-width cell descriptor", index, wantIndex, descriptor, ok)
			}
			if !indexed {
				path = 0
			}
		}
		if got, err := in.Global("path"); err != nil || got != path {
			t.Fatal("arriving reference target", got, path, err)
		}
		if got, err := in.Global("count"); err != nil || got != 3 {
			t.Fatal("real loop iterations", got, err)
		}
	}
	before := slices.Clone(args)
	if a := testing.AllocsPerRun(10, func() {
		got, err := in.Invoke("run", args...)
		if err != nil || !slices.Equal(got, want) || !slices.Equal(args, before) {
			panic("mixed reference controls")
		}
	}); a != 0 {
		t.Fatal("warm control allocation", a)
	}
}

func TestRV64PublicReferenceControlsRetiredImportedOwner(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, cell := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-cell%t", artifact, vector, cell), func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					t.Cleanup(func() { rv64HostCertificateCloseRuntime(t, rt) })
					typ := wasm.I32
					if vector {
						typ = wasm.V128
					}
					pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-controls-owner-v%t", vector), rv64PublicNativeIdentityModule([]wasm.ValType{typ}), artifact)
					producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
					fn, err := producer.ExportedFunc("identity")
					if err != nil {
						t.Fatal(err)
					}
					c := rv64NativeTailCompile(t, fmt.Sprintf("reference-controls-borrower-v%t-c%t", vector, cell), rv64ReferenceControlsSource(vector, true, false, cell, true, "br-nonnull"), artifact)
					in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Function("env", "target", fn))
					producer.Close()
					pc.Close()
					c.Close()
					runtime.GC()
					if !producer.hasPhysicalResources() {
						t.Fatal("control reference owner retired early")
					}
					rv64ReferenceControlsInvoke(t, in, vector, true, cell, true)
					state := &in.pluginState.Load().railshotInvocation
					if state.tableTargets[0] == nil || state.tableDepth != 0 || state.tailDispatcher != nil {
						t.Fatal("control owner scope")
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
					rv64HostCertificateCloseRuntime(t, rt)
					if producer.hasPhysicalResources() || in.hasPhysicalResources() || pc.codeCache.mem != nil || c.codeCache.mem != nil {
						t.Fatal("control final retirement")
					}
				})
			}
		}
	}
}

func TestRV64PublicReferenceControlsDeadSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, indexed := range []bool{false, true} {
				for _, nullable := range []bool{false, true} {
					name := fmt.Sprintf("reference-controls-dead-v%t-i%t-n%t", vector, indexed, nullable)
					t.Run(fmt.Sprintf("artifact%t-%s", artifact, name), func(t *testing.T) {
						typ := wasm.I32
						if vector {
							typ = wasm.V128
						}
						form, heap := byte(0x64), byte(0x70)
						if nullable {
							form = 0x63
						}
						if indexed {
							heap = 0
						}
						control := []byte{0x60, 1, form, heap, 1, form, heap}
						source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, wasm.I32}, []wasm.ValType{typ}), control)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 1, 4, 0x40, 0, 0x1b, 2, 1, 0x0b, 0x1a, 0x0b, 0x20, 0, 0x0b}))))
						c := rv64NativeTailCompile(t, name, source, artifact)
						witness := indexed || !nullable
						if c.nativeReferenceControlABI != witness || c.nativeReferenceCallABI || c.requiredFeatures.IsEnabled(CoreFeatureTypedFunctionReferences) != witness {
							t.Fatal("dead declaration-only witness")
						}
						p, r, err := railshotImportedCallSlots(c, 0, 0)
						if err != nil || p != 0 || r != 0 {
							t.Fatal("dead controls acquired call transport", p, r, err)
						}
						in, err := Instantiate(c)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						if witness && len(in.funcRefDescs) != 40 {
							t.Fatal("dead controls anchor extent", len(in.funcRefDescs))
						}
						args, want := rv64ElementGlobalArguments(vector)
						args = append(args, 0xcafebabe00000000)
						for range 3 {
							args[len(args)-1]++
							_, err := in.Invoke("run", args...)
							var trap *coreruntime.TrapError
							if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 5 {
								t.Fatal("dead indexed-control admission trap", err)
							}
							args[len(args)-1]--
							importedGlobalInvoke(t, in, "run", want, args...)
						}
						if a := testing.AllocsPerRun(10, func() {
							got, err := in.Invoke("run", args...)
							if err != nil || !slices.Equal(got, want) {
								panic("dead controls reuse")
							}
						}); a != 0 {
							t.Fatal("warm reuse allocation", a)
						}
					})
				}
			}
		}
	}
}

func rv64DeclarationOnlyOwnerSource(vector bool) []byte {
	typ, n := wasm.I32, 256
	if vector {
		typ, n = wasm.V128, 128
	}
	refs := wasmtest.ULEB(uint32(n))
	for range n {
		refs = append(refs, 0x63, 0)
	}
	control := append([]byte{0x60}, refs...)
	control = append(control, refs...)
	im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}), control)), wasmtest.Section(2, wasmtest.Vec(im)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x10, 0, 0x0b}))))
}

func TestRV64PublicReferenceControlsDeclarationOwner(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				t.Cleanup(func() { rv64HostCertificateCloseRuntime(t, rt) })
				typ := wasm.I32
				if vector {
					typ = wasm.V128
				}
				pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-controls-owner-v%t", vector), rv64PublicNativeIdentityModule([]wasm.ValType{typ}), artifact)
				producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
				fn, err := producer.ExportedFunc("identity")
				if err != nil {
					t.Fatal(err)
				}
				c := rv64NativeTailCompile(t, fmt.Sprintf("reference-controls-declaration-owner-v%t", vector), rv64DeclarationOnlyOwnerSource(vector), artifact)
				if !c.nativeReferenceControlABI || c.nativeReferenceCallABI || c.tableCount() != 0 || !railshotRequiresNativeOwnerScope(c) || rv64NativeTailModule(c) != nil {
					t.Fatal("declaration-only owner witness")
				}
				p, r, err := railshotImportedCallSlots(c, 0, 0)
				slots := 1
				if vector {
					slots = 2
				}
				if err != nil || p != slots || r != slots {
					t.Fatal("declaration-only import transport", p, r, err)
				}
				in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Function("env", "target", fn))
				producer.Close()
				pc.Close()
				c.Close()
				runtime.GC()
				if !producer.hasPhysicalResources() {
					t.Fatal("declaration-only foreign owner retired early")
				}
				args, want := rv64ElementGlobalArguments(vector)
				importedGlobalInvoke(t, in, "run", want, args...)
				if a := testing.AllocsPerRun(10, func() {
					got, err := in.Invoke("run", args...)
					if err != nil || !slices.Equal(got, want) {
						panic("declaration-only foreign call")
					}
				}); a != 0 {
					t.Fatal("declaration-only warm allocation", a)
				}
				state := &in.pluginState.Load().railshotInvocation
				if state.tableTargets[0] == nil || state.tableDepth != 0 {
					t.Fatal("declaration-only owner entry scope")
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
				rv64HostCertificateCloseRuntime(t, rt)
				if producer.hasPhysicalResources() || in.hasPhysicalResources() || pc.codeCache.mem != nil || c.codeCache.mem != nil {
					t.Fatal("declaration-only final retirement")
				}
			})
		}
	}
}
