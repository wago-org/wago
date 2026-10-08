//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64NativeTokenModule(vector, producer, rich, shared bool) []byte {
	typ := wasm.I32
	if vector {
		typ = wasm.V128
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ})))}
	var imports [][]byte
	if !producer {
		for _, name := range []string{"left", "right"} {
			imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name(name)...), 0, 0))
		}
	}
	if shared {
		imports = append(imports, rv64VectorMemoryImport())
	}
	if len(imports) != 0 {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(imports...)))
	}
	sections = append(sections, wasmtest.Section(3, []byte{1, 0}))
	if rich || !producer {
		sections = append(sections, wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}))
	}
	if !shared {
		sections = append(sections, wasmtest.Section(5, []byte{1, 1, 1, 4}))
	}
	var globals, exports [][]byte
	body := []byte{0x20, 0, 0x0b}
	if producer {
		exports = append(exports, wasmtest.ExportEntry("identity", 0, 0))
		if rich {
			globals = append(globals, wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd2, 0, 0x0b}))
			exports = append(exports, wasmtest.ExportEntry("reference", 3, 0))
		}
	} else {
		for index := 0; index < 256; index++ {
			expr := []byte{0xd2, byte(index), 0x0b}
			if index > 1 {
				expr = append(append([]byte{0x23}, wasmtest.ULEB(uint32(index-1))...), 0x0b)
			}
			globals = append(globals, wasmtest.GlobalEntry(wasm.FuncRef, false, expr))
		}
		exports = append(exports, wasmtest.ExportEntry("left", 3, 0), wasmtest.ExportEntry("right", 3, 1), wasmtest.ExportEntry("copy", 3, 255), wasmtest.ExportEntry("call", 0, 2))
		body = []byte{0x20, 0, 0x10, 0, 0x0b}
	}
	if len(globals) != 0 {
		sections = append(sections, wasmtest.Section(6, wasmtest.Vec(globals...)))
	}
	return wasmtest.Module(append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))...)
}

func rv64NativeTokenInstantiate(t *testing.T, rt *Runtime, c *Compiled, im *Imports) *Instance {
	t.Helper()
	mod, err := rt.Module(c)
	if err != nil {
		t.Fatal(err)
	}
	in, err := rt.Instantiate(context.Background(), mod, WithImports(im))
	mod.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close() })
	return in
}

func TestRV64PublicNativeReferenceTokens(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, rich := range []bool{false, true} {
			for _, shared := range []bool{false, true} {
				for _, retired := range []bool{false, true} {
					for _, artifact := range []bool{false, true} {
						t.Run(fmt.Sprintf("vector%t-rich%t-shared%t-retired%t-artifact%t", vector, rich, shared, retired, artifact), func(t *testing.T) {
							rt := NewRuntime()
							defer rt.Close()
							pc := rv64TableStageCompile(t, fmt.Sprintf("native-token-producer-vector%t-rich%t-shared%t", vector, rich, shared), rv64NativeTokenModule(vector, true, rich, shared), artifact)
							cc := rv64TableStageCompile(t, fmt.Sprintf("native-token-importer-vector%t-shared%t", vector, shared), rv64NativeTokenModule(vector, false, false, shared), artifact)
							pim, im := NewImports(), NewImports()
							if shared {
								mem, err := NewSharedMemory(1, 4)
								if err != nil {
									t.Fatal(err)
								}
								defer mem.Close()
								pim.Memory("env", "memory", mem)
								im.Memory("env", "memory", mem)
							}
							producer := rv64NativeTokenInstantiate(t, rt, pc, pim)
							if (len(producer.funcRefDescs) > 40) != rich || !rich && !rv64HeaderOnlyContext(producer) {
								t.Fatal("bare/descriptor producer fixture")
							}
							ex, err := producer.ExportedFunc("identity")
							if err != nil {
								t.Fatal(err)
							}
							im.Function("env", "left", ex).Function("env", "right", ex)
							first := rv64NativeTokenInstantiate(t, rt, cc, im)
							alias := rv64NativeTokenInstantiate(t, rt, cc, im)
							if (first.jm.LinMemBase() == producer.jm.LinMemBase()) != shared {
								t.Fatal("home identity fixture")
							}
							pc.Close()
							cc.Close()
							var want Value
							if rich && !retired {
								want, err = producer.GlobalValue("reference")
								if err != nil {
									t.Fatal(err)
								}
							}
							if retired {
								producer.Close()
							}
							token, err := first.GlobalValue("left")
							if err != nil || token.FuncRef().IsNull() {
								t.Fatal("native proxy token", token, err)
							}
							if rich && !retired && token != want {
								t.Fatal("producer/importer identity", want, token)
							}
							for _, owner := range []*Instance{first, alias} {
								for _, name := range []string{"left", "right", "copy"} {
									got, err := owner.GlobalValue(name)
									if err != nil || got != token {
										t.Fatal("alias/copy identity", name, got, err)
									}
								}
								if !owner.FuncRefMatchesFunction(token.FuncRef(), 0) || !owner.FuncRefMatchesFunction(token.FuncRef(), 1) || owner.FuncRefMatchesFunction(token.FuncRef(), 2) || owner.FuncRefMatchesFunction(token.FuncRef(), 3) {
									t.Fatal("proxy semantic function identity")
								}
							}
							if !retired && !producer.FuncRefMatchesFunction(token.FuncRef(), 0) {
								t.Fatal("bare/local producer identity")
							}
							if len(rt.refStore.byToken) != 1 {
								t.Fatal("duplicate identity token allocation")
							}
							descriptor := uint64(offHeapSlicePtr(first.funcRefDescs)) + coreruntime.FuncRefDescBytes
							if allocs := testing.AllocsPerRun(20, func() {
								if _, err := rt.refStore.issue(first, descriptor); err != nil {
									t.Fatal(err)
								}
							}); allocs != 0 {
								t.Fatal("cached proxy issuance allocations", allocs)
							}
							if allocs := testing.AllocsPerRun(20, func() {
								if !alias.FuncRefMatchesFunction(token.FuncRef(), 1) {
									t.Fatal("cached alias identity")
								}
							}); allocs != 0 {
								t.Fatal("alias identity allocations", allocs)
							}
							held, err := rt.NewFuncRefGlobal(token.FuncRef(), false)
							if err != nil {
								t.Fatal(err)
							}
							defer held.Close()
							producer.Close()
							first.Close()
							runtime.GC()
							if !producer.hasPhysicalResources() {
								t.Fatal("producer lost attachment/token root")
							}
							if !rich || retired {
								if !first.hasPhysicalResources() || len(first.thunkMem) == 0 {
									t.Fatal("token lost owned wrapper")
								}
							}
							args, wantCall := []uint64{0xfeedface00000007}, []uint64{7}
							if vector {
								args = []uint64{0x0123456789abcdef, 0xfedcba9876543210}
								wantCall = args
							}
							importedGlobalInvoke(t, alias, "call", wantCall, args...)
							if got, err := alias.GlobalValue("copy"); err != nil || got != token {
								t.Fatal("retired identity", got, err)
							}
							alias.Close()
							if got, err := held.GetValue(); err != nil || got != token {
								t.Fatal("host-held proxy token", got, err)
							}
							held.Close()
							ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()
							if err := rt.CloseContext(ctx); err != nil {
								t.Fatal(err)
							}
							if producer.hasPhysicalResources() || first.hasPhysicalResources() || alias.hasPhysicalResources() || pc.codeCache.mem != nil || cc.codeCache.mem != nil {
								t.Fatal("final token/store mapping release")
							}
						})
					}
				}
			}
		}
	}
}

func TestRV64PublicNativeReferenceTokenAuthentication(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			pc := rv64TableStageCompile(t, "native-token-producer-vectorfalse-richfalse-sharedfalse", rv64NativeTokenModule(false, true, false, false), artifact)
			cc := rv64TableStageCompile(t, "native-token-importer-vectorfalse-sharedfalse", rv64NativeTokenModule(false, false, false, false), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
			unrelated := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
			ex, err := producer.ExportedFunc("identity")
			if err != nil {
				t.Fatal(err)
			}
			in := rv64NativeTokenInstantiate(t, rt, cc, NewImports().Function("env", "left", ex).Function("env", "right", ex))
			row := in.funcRefDescs[coreruntime.FuncRefDescBytes : 2*coreruntime.FuncRefDescBytes]
			original := slices.Clone(row)
			var token Value
			for _, cached := range []bool{false, true} {
				if cached {
					token, err = in.GlobalValue("left")
					if err != nil {
						t.Fatal(err)
					}
					if unrelated.FuncRefMatchesFunction(token.FuncRef(), 0) {
						t.Fatal("same code mapping confused distinct producer identities")
					}
				}
				before := len(rt.refStore.byToken)
				for _, offset := range []int{0, 8, 16, 24, 32} {
					binary.LittleEndian.PutUint64(row[offset:], binary.LittleEndian.Uint64(original[offset:])^1)
					_, err := in.GlobalValue("left")
					matches := cached && in.FuncRefMatchesFunction(token.FuncRef(), 0)
					copy(row, original)
					if err == nil || matches || len(rt.refStore.byToken) != before {
						t.Fatal("unauthenticated proxy accepted", cached, offset, err)
					}
				}
			}
		})
	}
}

func TestRV64PublicNativeReferenceTokenBindingGate(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt, foreign := NewRuntime(), NewRuntime()
			defer rt.Close()
			defer foreign.Close()
			pc := rv64TableStageCompile(t, "native-token-producer-vectorfalse-richfalse-sharedfalse", rv64NativeTokenModule(false, true, false, false), artifact)
			cc := rv64TableStageCompile(t, "native-token-importer-vectorfalse-sharedfalse", rv64NativeTokenModule(false, false, false, false), artifact)
			producer := rv64NativeTokenInstantiate(t, foreign, pc, NewImports())
			ex, err := producer.ExportedFunc("identity")
			if err != nil {
				t.Fatal(err)
			}
			plain := slotHostFunc(func(_ HostModule, p, r []uint64) { r[0] = p[0] })
			host, err := rt.NewHostFuncRef(plain, FuncSig{Params: []ValType{ValI64}, Results: []ValType{ValI64}})
			if err != nil {
				t.Fatal(err)
			}
			defer host.Close()
			standalone, err := Instantiate(pc)
			if err != nil {
				t.Fatal(err)
			}
			defer standalone.Close()
			standaloneExport, err := standalone.ExportedFunc("identity")
			if err != nil {
				t.Fatal(err)
			}
			type mappingState struct {
				base, mem      uintptr
				length, refs   int
				closed, sealed bool
				flags          compiledCodeCacheFlags
			}
			mapping := func() mappingState {
				cache := cc.codeCache
				cache.mu.Lock()
				defer cache.mu.Unlock()
				return mappingState{cache.base, offHeapSlicePtr(cache.mem), len(cache.mem), cache.refs, cache.closed, cache.sealed, cache.flags}
			}
			roots := producer.referenceLifetime().snapshot().ResourceRoots
			standaloneRoots := standalone.referenceLifetime().snapshot().ResourceRoots
			for _, binding := range []any{plain, host, ex, standaloneExport} {
				imports := NewImports().Function("env", "left", binding).Function("env", "right", binding)
				mod, err := rt.Module(cc)
				if err != nil {
					t.Fatal(err)
				}
				before := mapping()
				in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
				unchanged := mapping() == before
				mod.Close()
				if in != nil {
					in.Close()
				}
				if err == nil || in != nil {
					t.Fatal("unsupported token binding published", binding, err)
				}
				host.mu.Lock()
				hostImporters := host.importers
				host.mu.Unlock()
				if !unchanged || hostImporters != 0 || producer.referenceLifetime().snapshot().ResourceRoots != roots || standalone.referenceLifetime().snapshot().ResourceRoots != standaloneRoots || len(rt.refStore.byToken) != 0 || len(rt.refStore.byIdentity) != 0 || standalone.refStore != nil {
					t.Fatal("failed binding retained owner/token")
				}
			}
			before := mapping()
			in, err := Instantiate(cc, NewImports().Function("env", "left", ex).Function("env", "right", ex))
			if in != nil {
				in.Close()
			}
			if err == nil || in != nil {
				t.Fatal("standalone exported imported token admitted", err)
			}
			if mapping() != before || producer.referenceLifetime().snapshot().ResourceRoots != roots {
				t.Fatal("standalone rejection changed mapping/owner")
			}
		})
	}
}
