//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceImportModule(vector, borrowed bool) []byte {
	typ := wasm.I32
	if vector {
		typ = wasm.V128
	}
	imports := [][]byte{wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, false), wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, false)}
	if borrowed {
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 2, 5))
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType([]wasm.ValType{typ, wasm.I32}, []wasm.ValType{typ}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, nil))),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
		wasmtest.Section(3, []byte{4, 1, 2, 3, 4}),
	}
	if !borrowed {
		sections = append(sections, wasmtest.Section(4, []byte{1, 0x70, 1, 2, 5}))
	}
	globals := make([][]byte, 254)
	for i := range globals {
		expr := append(append([]byte{0x23}, wasmtest.ULEB(uint32(i+1))...), 0x0b)
		globals[i] = wasmtest.GlobalEntry(wasm.FuncRef, false, expr)
	}
	sections = append(sections,
		wasmtest.Section(6, wasmtest.Vec(globals...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("install", 0, 0), wasmtest.ExportEntry("call", 0, 1), wasmtest.ExportEntry("isnull", 0, 2), wasmtest.ExportEntry("clear", 0, 3), wasmtest.ExportEntry("table", 1, 0), wasmtest.ExportEntry("import", 3, 0), wasmtest.ExportEntry("duplicate", 3, 1), wasmtest.ExportEntry("copy", 3, 255))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x23, 0xff, 1, 0x26, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 0, 0, 0x0b}),
			wasmtest.Code([]byte{0x23, 0xff, 1, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd0, 0x70, 0x20, 1, 0xfc, 17, 0, 0x0b}),
		)),
	)
	return wasmtest.Module(sections...)
}

func TestRV64PublicReferenceImportIndex255(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				rt := NewRuntime()
				defer rt.Close()
				pc := rv64TableStageCompile(t, fmt.Sprintf("ref-import-producer-vector%t-local", vector), rv64NativeTokenModule(vector, true, true, false), artifact)
				producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
				g, err := producer.ExportedGlobalObject("reference")
				if err != nil {
					t.Fatal(err)
				}
				imports := make([][]byte, 256)
				for i := range imports {
					imports[i] = wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, false)
				}
				source := rv64ReplaceTableStageSection(t, rv64ReferenceImportModule(vector, false), 2, wasmtest.Section(2, wasmtest.Vec(imports...)))
				source = rv64ReplaceTableStageSection(t, source, 6, nil)
				c := rv64TableStageCompile(t, fmt.Sprintf("ref-import-index255-vector%t", vector), source, artifact)
				in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Global("env", "ref", g))
				if in.globalCells[255] != g || len(in.importGlobalReferences()) != 1 {
					t.Fatal("last imported cell or deduplication")
				}
				producer.Close()
				pc.Close()
				c.Close()
				if _, err := in.Invoke("install", 0); err != nil {
					t.Fatal(err)
				}
				args, want := []uint64{0xfeedface00000007, 0}, []uint64{7}
				if vector {
					args = []uint64{0x0123456789abcdef, 0xfedcba9876543210, 0}
					want = args[:2]
				}
				importedGlobalInvoke(t, in, "call", want, args...)
				if v, err := in.GlobalValue("copy"); err != nil || v.FuncRef().IsNull() {
					t.Fatal("imported index255 egress", v, err)
				}
				in.Close()
			})
		}
	}
}

func TestRV64PublicReferenceImportUnusedSourceAndLimits(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source rejection")
	}
	for _, vector := range []bool{false, true} {
		for _, tc := range []struct {
			name                     string
			typ                      wasm.ValType
			mutable, owned, borrowed bool
			count                    int
		}{
			{"unused-mutable", wasm.FuncRef, true, true, false, 1},
			{"unused-nonnullable", wasm.RefVal(wasm.Ref(false, wasm.AbsHeap(wasm.HeapFunc), false)), false, true, false, 1},
			{"unused-typed", wasm.RefVal(wasm.Ref(true, wasm.IndexedHeap(wasm.TypeIdx{Index: 0}), false)), false, true, false, 1},
			{"unused-externref", wasm.ExternRef, false, true, false, 1},
			{"unused-count", wasm.FuncRef, false, true, false, 257},
		} {
			imports := make([][]byte, tc.count)
			for i := range imports {
				if tc.name == "unused-nonnullable" || tc.name == "unused-typed" {
					encoded := []byte{0x64, 0x70} // (ref func)
					if tc.name == "unused-typed" {
						encoded = []byte{0x63, 0} // (ref null 0)
					}
					entry := append(wasmtest.Name("env"), wasmtest.Name("ref")...)
					entry = append(entry, 3)
					imports[i] = append(append(entry, encoded...), 0)
				} else {
					imports[i] = wasmtest.GlobalImportEntry("env", "ref", tc.typ, tc.mutable)
				}
			}
			if tc.borrowed {
				imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 2, 5))
			}
			typ := wasm.I32
			body := []byte{0x41, 0, 0x0b}
			if vector {
				typ = wasm.V128
				body = append([]byte{0xfd, 12}, make([]byte, 16)...)
				body = append(body, 0x0b)
			}
			sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{typ}))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{1, 0})}
			if tc.owned {
				sections = append(sections, wasmtest.Section(4, []byte{1, 0x70, 1, 2, 5}))
			}
			source := wasmtest.Module(append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))...)
			m, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			if err := wasm.ValidateModuleWithConfig(m, wasm.ValidationFeatures{}, 1, wasm.ValidationLimits{}); err != nil {
				t.Fatal("negative fixture invalid before RV64 admission", tc.name, err)
			}
			image, err := railshot.CompileModuleWith(m, railshot.CompileOptions{DeferCodeMapping: true, Interruptible: true, ForeignTableCalls: true, InvocationContext: true, NativeReferenceFunctions: true, NativeReferenceCalls: true})
			if image != nil && image.CodeImage != nil {
				image.CodeImage.Close()
			}
			if image == nil || err != nil {
				t.Fatal("valid decoded unused import rejected", tc.name, vector, err)
			}
			cfg := NewRuntimeConfig().WithCoreFeatures(platformCoreFeatures() | CoreFeatureTypedFunctionReferences | CoreFeatureGC)
			c, err := cfg.Compile(source)

			if err != nil {
				t.Fatal("valid unused reference import rejected", tc.name, vector, err)
			}

			c.Close()
			if err != nil {
				t.Fatal("unused reference import artifact", tc.name, vector, err)
			}

			c.Close()
		}
	}
}

func TestRV64PublicReferenceImportBindingGate(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			other := NewRuntime()
			defer other.Close()
			foreign, err := other.NewFuncRefGlobal(NullFuncRef(), false)
			if err != nil {
				t.Fatal(err)
			}
			defer foreign.Close()
			mutable, err := rt.NewFuncRefGlobal(NullFuncRef(), true)
			if err != nil {
				t.Fatal(err)
			}
			defer mutable.Close()
			closed, err := rt.NewFuncRefGlobal(NullFuncRef(), false)
			if err != nil {
				t.Fatal(err)
			}
			closed.Close()
			c := rv64TableStageCompile(t, "ref-import-consumer-vectorfalse", rv64ReferenceImportModule(false, false), artifact)
			type mappingState struct {
				base, mem      uintptr
				length, refs   int
				closed, sealed bool
				flags          compiledCodeCacheFlags
			}
			mapping := func() mappingState {
				cache := c.codeCache
				cache.mu.Lock()
				defer cache.mu.Unlock()
				return mappingState{cache.base, offHeapSlicePtr(cache.mem), len(cache.mem), cache.refs, cache.closed, cache.sealed, cache.flags}
			}
			for _, binding := range []any{foreign, mutable, closed, ValueFuncRef(NullFuncRef()), &Global{Type: ValFuncRef}} {
				mod, err := rt.Module(c)
				if err != nil {
					t.Fatal(err)
				}
				before := mapping()
				in, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Global("env", "ref", binding)))
				unchanged := mapping() == before
				mod.Close()
				if in != nil {
					in.Close()
				}
				if err == nil || in != nil || !unchanged || len(rt.refStore.byToken) != 0 || len(rt.refStore.byIdentity) != 0 || foreign.owner.importers != 0 || mutable.owner.importers != 0 {
					t.Fatal("invalid reference binding published or retained", binding, err)
				}
			}
			local, err := rt.NewFuncRefGlobal(NullFuncRef(), false)
			if err != nil {
				t.Fatal(err)
			}
			defer local.Close()
			before := mapping()
			in, err := Instantiate(c, NewImports().Global("env", "ref", local))
			if in != nil {
				in.Close()
			}
			if err == nil || in != nil || mapping() != before || local.owner.importers != 0 {
				t.Fatal("standalone reference import", err)
			}
		})
	}
}

func TestRV64PublicReferenceImportAuthenticationAndRollback(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			pc := rv64TableStageCompile(t, "ref-import-producer-vectorfalse-local", rv64NativeTokenModule(false, true, true, false), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
			g, err := producer.ExportedGlobalObject("reference")
			if err != nil {
				t.Fatal(err)
			}
			c := rv64TableStageCompile(t, "ref-import-consumer-vectorfalse", rv64ReferenceImportModule(false, false), artifact)
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Global("env", "ref", g))
			descriptor := readGlobalObject(g, ValFuncRef)
			if descriptor <= 0xffffffff {
				t.Fatal("not a full-width reference fixture")
			}
			for _, cached := range []bool{false, true} {
				if cached {
					if _, err := in.GlobalValue("copy"); err != nil {
						t.Fatal(err)
					}
				}
				tokens := len(rt.refStore.byToken)
				for _, offset := range []int{0, 8, 16, 24, 32} {
					entry, _ := rv64PublishedDescriptor(producer, descriptor)
					saved := binary.LittleEndian.Uint64(entry[offset:])
					binary.LittleEndian.PutUint64(entry[offset:], saved^1)
					_, egressErr := in.GlobalValue("copy")
					_, invokeErr := in.Invoke("isnull")
					binary.LittleEndian.PutUint64(entry[offset:], saved)
					if egressErr == nil || invokeErr == nil || len(rt.refStore.byToken) != tokens {
						t.Fatal("corrupt descriptor accepted", cached, offset, egressErr, invokeErr)
					}
				}
				writeGlobalObject(g, ValFuncRef, 1)
				_, egressErr := in.GlobalValue("import")
				_, invokeErr := in.Invoke("isnull")
				writeGlobalObject(g, ValFuncRef, descriptor)
				if egressErr == nil || invokeErr == nil {
					t.Fatal("changed immutable cell accepted")
				}
			}
			roots := producer.referenceLifetime().snapshot().ResourceRoots
			var cleanup resourceCleanup
			entries, err := railshotFreezeGlobalReferences(&Compiled{GlobalImports: []GlobalImportDef{{Type: ValFuncRef}, {Type: ValFuncRef}}}, []*resolvedGlobalImport{{global: g}, {global: &Global{Type: ValFuncRef}}}, rt.refStore, &cleanup)
			cleanup.finish()
			if err == nil || entries != nil || producer.referenceLifetime().snapshot().ResourceRoots != roots {
				t.Fatal("partial frozen-root rollback", err)
			}
			imports := g.owner.importers
			instances := len(rt.refStore.instances)
			sentinel := errors.New("after-create rejection")
			mod, err := rt.Module(c)
			if err != nil {
				t.Fatal(err)
			}
			rt.hooks.afterCreate = append(rt.hooks.afterCreate, func(InstantiationEvent) error { return sentinel })
			failed, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Global("env", "ref", g)))
			rt.hooks.afterCreate = rt.hooks.afterCreate[:len(rt.hooks.afterCreate)-1]
			mod.Close()
			if failed != nil || !errors.Is(err, sentinel) || producer.referenceLifetime().snapshot().ResourceRoots != roots || g.owner.importers != imports || len(rt.refStore.instances) != instances {
				t.Fatal("construction rollback", err)
			}
			in.Close()
		})
	}
}

func TestRV64PublicImmutableReferenceImports(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, kind := range []string{"local", "native", "host", "null", "store", "host-alias"} {
			if vector && (kind == "host" || kind == "host-alias") {
				continue // Explicit hosts keep the separately qualified scalar ABI.
			}
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-%s-artifact%t", vector, kind, artifact), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					var providers []*Instance
					var images []*Compiled
					var global *Global
					var host *HostFuncRef
					var err error
					var canonical Value
					if kind == "null" {
						global, err = rt.NewFuncRefGlobal(NullFuncRef(), false)
						if err != nil {
							t.Fatal(err)
						}
						defer global.Close()
					} else {
						var c *Compiled
						im := NewImports()
						name := "reference"
						if kind == "host" || kind == "host-alias" {
							c = rv64HostTokenCompile(t, 0, artifact)
							host = rv64HostTokenOwner(t, rt, ValI32, slotHostFunc(func(_ HostModule, p, r []uint64) { copy(r, p); runtime.GC() }))
							im = rv64HostTokenImports(host)
							name = "copy"
							if kind == "host-alias" {
								first := rv64NativeTokenInstantiate(t, rt, c, im)
								providers = append(providers, first)
								canonical, err = first.GlobalValue("copy")
								if err != nil {
									t.Fatal(err)
								}
							}
						} else {
							c = rv64TableStageCompile(t, fmt.Sprintf("ref-import-producer-vector%t-%s", vector, kind), rv64NativeTokenModule(vector, kind == "local" || kind == "store", true, false), artifact)
							if kind == "native" {
								leafCode := rv64TableStageCompile(t, fmt.Sprintf("ref-import-leaf-vector%t", vector), rv64NativeTokenModule(vector, true, false, false), artifact)
								leaf := rv64NativeTokenInstantiate(t, rt, leafCode, NewImports())
								providers = append(providers, leaf)
								images = append(images, leafCode)
								ex, err := leaf.ExportedFunc("identity")
								if err != nil {
									t.Fatal(err)
								}
								im.Function("env", "left", ex).Function("env", "right", ex)
								name = "copy"
							}
						}
						producer := rv64NativeTokenInstantiate(t, rt, c, im)
						providers = append(providers, producer)
						images = append(images, c)
						if kind == "store" {
							canonical, err = producer.GlobalValue(name)
							if err != nil {
								t.Fatal(err)
							}
							if !producer.usesIndependentExecution() {
								t.Fatal("store fixture already shared")
							}
							global, err = rt.NewFuncRefGlobal(canonical.FuncRef(), false)
							if err != nil {
								t.Fatal(err)
							}
							defer global.Close()
						} else {
							global, err = producer.ExportedGlobalObject(name)
							if err != nil {
								t.Fatal(err)
							}
						}
					}
					tokens := len(rt.refStore.byToken)
					c := rv64TableStageCompile(t, fmt.Sprintf("ref-import-consumer-vector%t", vector), rv64ReferenceImportModule(vector, false), artifact)
					in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Global("env", "ref", global))
					if len(in.importGlobalReferences()) != 1 || in.globalCells[0] != in.globalCells[1] || in.globalCells[0] == in.globalCells[255] {
						t.Fatal("duplicate attachment or copied cell layout")
					}
					if len(rt.refStore.byToken) != tokens {
						t.Fatal("construction pre-issued token")
					}
					if kind == "store" && !providers[0].nativeControlIsShared() {
						t.Fatal("imported token owner kept independent native execution")
					}
					if kind == "host-alias" && in.importGlobalReferences()[0].owner != providers[len(providers)-1] {
						t.Fatal("frozen root does not own the raw proxy slot")
					}
					for _, producer := range providers {
						producer.Close()
					}
					for _, image := range images {
						image.Close()
					}
					c.Close()
					if kind != "null" && kind != "store" {
						if _, err := global.GetValue(); err == nil {
							t.Fatal("closed producer direct access")
						}
					}
					value, err := in.GlobalValue("copy")
					if err != nil || value.FuncRef().IsNull() != (kind == "null") {
						t.Fatal("first copied egress after retirement", value, err)
					}
					if (kind == "store" || kind == "host-alias") && value != canonical {
						t.Fatal("canonical token changed", value, canonical)
					}
					for _, name := range []string{"import", "duplicate", "copy"} {
						got, err := in.GlobalValue(name)
						if err != nil || got != value {
							t.Fatal("import alias identity", got, err)
						}
					}
					copyCell, err := in.ExportedGlobalObject("copy")
					if err != nil {
						t.Fatal(err)
					}
					if got, err := copyCell.GetValue(); err != nil || got != value {
						t.Fatal("copied cell object egress", got, err)
					}
					if _, err := in.Invoke("install", 0); err != nil {
						t.Fatal(err)
					}
					args, want := []uint64{0xdeadbeef00000007, 0}, []uint64{7}
					if vector {
						args = []uint64{0x0123456789abcdef, 0xfedcba9876543210, 0}
						want = args[:2]
					}
					if kind != "null" {
						importedGlobalInvoke(t, in, "call", want, args...)
					}
					table, err := in.ExportedTable("table")
					if err != nil {
						t.Fatal(err)
					}
					rc := rv64TableStageCompile(t, fmt.Sprintf("ref-import-reader-vector%t", vector), rv64ReferenceGlobalModule(vector, true, false, true, 1), artifact)
					reader := rv64NativeTokenInstantiate(t, rt, rc, NewImports().Table("env", "table", table))
					rc.Close()
					in.Close()
					runtime.GC()
					if !in.hasPhysicalResources() {
						t.Fatal("table owner lost reference attachments")
					}
					if kind == "null" {
						_, err := reader.Invoke("call", args...)
						tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
					} else {
						importedGlobalInvoke(t, reader, "call", want, args...)
					}
					if _, err := reader.Invoke("fill", 0, 2); err != nil {
						t.Fatal(err)
					}
					reader.Close()
					if kind == "null" || kind == "store" {
						if err := global.Close(); err != nil {
							t.Fatal(err)
						}
					}
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if err := rt.CloseContext(ctx); err != nil {
						t.Fatal(err)
					}
					if host != nil {
						if err := host.Close(); err != nil {
							t.Fatal(err)
						}
					}
					for _, producer := range append(providers, in, reader) {
						if producer.hasPhysicalResources() {
							t.Fatal("reference import retained physical resources")
						}
					}
					for _, image := range append(images, c, rc) {
						if image.codeCache.mem != nil {
							t.Fatal("reference import retained code mapping")
						}
					}
				})
			}
		}
	}
}

// Keep a foreign reference in a guest local while the only table row is null
// and Go owns the stack. The producer may retire before guest execution resumes.
func TestRV64PublicReferenceImportParkedLifetime(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			for _, action := range []string{"resume", "cancel", "close"} {
				t.Run(fmt.Sprintf("vector%t-artifact%t-%s", vector, artifact, action), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					pc := rv64TableStageCompile(t, fmt.Sprintf("ref-import-producer-vector%t-local", vector), rv64NativeTokenModule(vector, true, true, false), artifact)
					producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
					g, err := producer.ExportedGlobalObject("reference")
					if err != nil {
						t.Fatal(err)
					}
					typ := wasm.I32
					if vector {
						typ = wasm.V128
					}
					parkImport := append(append(wasmtest.Name("env"), wasmtest.Name("park")...), 0, 1)
					spin := []byte{1, 1, 0x70, 0x23, 0, 0x21, 0, 0x41, 0, 0xd0, 0x70, 0x26, 0, 0x10, 0, 0x41, 0, 0x20, 0, 0x26, 0, 0x0b}
					source := wasmtest.Module(
						wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}), wasmtest.FuncType(nil, nil))),
						wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, false), parkImport)),
						wasmtest.Section(3, []byte{2, 1, 0}),
						wasmtest.Section(4, []byte{1, 0x70, 1, 2, 5}),
						wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("spin", 0, 1), wasmtest.ExportEntry("call", 0, 2))),
						wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(spin))), spin...), wasmtest.Code([]byte{0x20, 0, 0x41, 0, 0x11, 0, 0, 0x0b}))),
					)
					c := rv64TableStageCompile(t, fmt.Sprintf("ref-import-park-vector%t", vector), source, artifact)
					entered, release := make(chan struct{}, 1), make(chan struct{})
					var once sync.Once
					unblock := func() { once.Do(func() { close(release) }) }
					defer unblock()
					park := func() { entered <- struct{}{}; <-release; runtime.GC() }
					in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Global("env", "ref", g).Function("env", "park", park))
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					done := make(chan error, 1)
					go func() { _, err := in.InvokeContext(ctx, "spin"); done <- err }()
					joined := false
					defer func() {
						cancel()
						unblock()
						if !joined {
							<-done
						}
					}()
					select {
					case <-entered:
					case <-time.After(5 * time.Second):
						t.Fatal("reference carrier did not park")
					}
					producer.Close()
					pc.Close()
					c.Close()
					row := unsafe.Slice((*byte)(offHeapPtr(in.tableDescPtr)), in.tableDescLen)[8:40]
					if !slices.Equal(row, make([]byte, 32)) || !producer.hasPhysicalResources() || len(rt.refStore.byToken) != 0 {
						t.Fatal("parked reference lost its unissued owner or null row")
					}
					if action == "cancel" {
						cancel()
					}
					if action == "close" {
						in.Close()
					}
					if action != "resume" {
						deadline := time.Now().Add(5 * time.Second)
						for atomic.LoadUint32((*uint32)(unsafe.Pointer(&in.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
							runtime.Gosched()
						}
						if atomic.LoadUint32((*uint32)(unsafe.Pointer(&in.trap[0]))) != uint32(coreruntime.TrapInterrupted) {
							t.Fatal("parked interruption was not published")
						}
					}
					unblock()
					select {
					case err := <-done:
						joined = true
						if action == "resume" && err != nil || action != "resume" && err == nil || action == "cancel" && !errors.Is(err, context.Canceled) {
							t.Fatal("parked result", action, err)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("parked reference did not unwind")
					}
					if action == "resume" {
						args, want := []uint64{0xdeadbeef00000007}, []uint64{7}
						if vector {
							args = []uint64{0x0123456789abcdef, 0xfedcba9876543210}
							want = args
						}
						importedGlobalInvoke(t, in, "call", want, args...)
					} else if action == "cancel" && !slices.Equal(row, make([]byte, 32)) {
						t.Fatal("cancelled guest published its saved reference")
					}
					in.Close()
					closeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
					defer stop()
					if err := rt.CloseContext(closeCtx); err != nil {
						t.Fatal(err)
					}
					if producer.hasPhysicalResources() || in.hasPhysicalResources() || pc.codeCache.mem != nil || c.codeCache.mem != nil {
						t.Fatal("parked reference retained resources")
					}
				})
			}
		}
	}
}
