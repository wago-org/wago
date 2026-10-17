//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64HostCertificateCloseRuntime(t *testing.T, rt *Runtime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rt.CloseContext(ctx); err != nil {
		t.Error("host certificate runtime cleanup", err)
	}
}

func TestRV64HostCertificateMappingBindingAndLifetime(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, owned := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-owned%t", artifact, owned), func(t *testing.T) {
				types := []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
				c := rv64NativeTailCompile(t, "host-certificate-ordinary", rv64PublicReferenceTailModule(types, true, true), artifact)
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rv64HostCertificateCloseRuntime(t, rt)
				makeInstance := func(add uint64) (*Instance, *HostFuncRef) {
					var callback any = HostCallFunc(func(call HostCall) { copy(call.ResultSlots(), call.ParamSlots()); call.ResultSlots()[2] += add })
					var ref *HostFuncRef
					if owned {
						var err error
						ref, err = rt.NewHostFuncRef(callback, c.importFuncSigs[0])
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { _ = ref.Close() })
						callback = ref
					}
					return rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "target", callback)), ref
				}
				in, ref := makeInstance(1)
				sibling, _ := makeInstance(2)
				if _, err := rv64NewHostTailCodeOwner(in, 0); !errors.Is(err, railshot.ErrUnsupported) {
					t.Fatal("unleased host owner accepted", err)
				}
				if !in.retainResourceRoot() {
					t.Fatal("physical importer root")
				}
				root := true
				defer func() {
					if root {
						in.releaseResourceRoot()
					}
				}()
				var owner *rv64HostTailCodeOwner
				var image *railshot.NativeTailCodeImage
				var target *railshot.NativeTailHostFunction
				err := rv64GenerationLockedForTest(func() error {
					var err error
					owner, err = rv64NewHostTailCodeOwner(in, 0)
					if err != nil {
						return err
					}
					image, err = railshot.NewNativeTailCodeImage(owner)
					if err != nil {
						return err
					}
					target, err = railshot.NewNativeTailHostFunction(image, owner.record.certificate, in.jm, rv64NativeInvocationContext(in), in.funcRefDescs, owner.record.offset, owner.record.dispatchIndex, owned, wasm.CompType{Kind: wasm.CompFunc, Params: types, Results: types})
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				if len(image.Bytes()) == 0 || cap(image.Bytes()) != len(image.Bytes()) {
					t.Fatal("logical wrapper span")
				}
				if !owned && in.importReferenceEntries()[0].address != sibling.importReferenceEntries()[0].address {
					t.Fatal("shared wrapper fixture")
				}
				for _, tc := range []struct {
					instance *Instance
					result   uint64
				}{{in, 50}, {sibling, 51}} {
					got, err := tc.instance.Invoke("call", 0xcafe000080000001, 0xcafe00007f800123, 49, 0x7ff0000000000001, 0)
					if err != nil || !slices.Equal(got, []uint64{0x80000001, 0x7f800123, tc.result, 0x7ff0000000000001}) {
						t.Fatal("particular host callback", got, err)
					}
				}
				validate := func() error { return rv64GenerationLockedForTest(target.Validate) }
				if n := testing.AllocsPerRun(4, func() {
					if err := validate(); err != nil {
						panic(err)
					}
				}); n != 0 {
					t.Fatal("warm host owner validation allocated", n)
				}
				for _, mutation := range []string{"callback-slot", "signature", "descriptor", "context", "certificate", "dispatch", "logical-span", "sibling-instance"} {
					t.Run(mutation, func(t *testing.T) {
						savedOwner := *owner
						savedHosts := in.syncHosts
						savedContext := in.nativeContext
						savedEntry := in.importReferenceEntries()[0]
						savedType := in.c.importFuncSigs[0].Params[0]
						var construction *railshotHostThunkConstruction
						if owned {
							construction = &in.pluginState.Load().railshotInvocation.nativeImports.hosts
						} else {
							construction = &in.c.loadValidateMemo().hostThunks[1].railshotHostThunkConstruction
						}
						savedConstruction := *construction
						savedRecord := construction.records[0]
						defer func() {
							*owner = savedOwner
							in.syncHosts = savedHosts
							in.nativeContext = savedContext
							in.importState.Load().importRefEntries[0] = savedEntry
							in.c.importFuncSigs[0].Params[0] = savedType
							*construction = savedConstruction
							construction.records[0] = savedRecord
						}()
						switch mutation {
						case "callback-slot":
							in.syncHosts = slices.Clone(in.syncHosts)
						case "signature":
							in.c.importFuncSigs[0].Params[0] = ValI64
						case "descriptor":
							in.importState.Load().importRefEntries[0].address += 16
						case "context":
							in.nativeContext = 0
						case "certificate":
							construction.records[0].certificate = nil
						case "dispatch":
							construction.records[0].dispatchIndex++
						case "logical-span":
							construction.codeBytes--
						case "sibling-instance":
							owner.instance = sibling
						}
						if err := validate(); !errors.Is(err, railshot.ErrUnsupported) {
							t.Fatal("changed host owner accepted", err)
						}
					})
				}
				if ref != nil {
					if err := ref.Close(); err == nil {
						t.Fatal("owned callback closed with retained importer")
					}
				}
				if err := in.c.Close(); err != nil {
					t.Fatal(err)
				}
				if err := in.Close(); err != nil || in.resourcesClosed {
					t.Fatal("logical importer close", err)
				}
				if err := validate(); err != nil {
					t.Fatal("retained callback/image lost after logical close", err)
				}
				root = false
				in.releaseResourceRoot()
				if !in.resourcesClosed {
					t.Fatal("importer physical resources retained")
				}
				if err := validate(); !errors.Is(err, railshot.ErrUnsupported) {
					t.Fatal("sibling cache lease kept stale callback live", err)
				}
				if ref != nil {
					if err := ref.Close(); err != nil {
						t.Fatal("owned callback retained after physical importer teardown", err)
					}
				}
			})
		}
	}
}

func rv64MixedHostCertificateModule() []byte {
	var imports [][]byte
	for _, name := range []string{"native", "go", "owned"} {
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name(name)...), 0, 0))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{1, 1}),
		wasmtest.Section(4, []byte{1, 0x70, 0, 3}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 3))),
		wasmtest.Section(9, []byte{1, 0, 0x41, 0, 0x0b, 3, 0, 1, 2}),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 0, 0, 0x0b}))))
}

func TestRV64HostCertificateMixedPublication(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			producer, err := Instantiate(rv64NativeTailCompile(t, "host-certificate-mixed-producer", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact))
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			export, err := producer.ExportedFunc("identity")
			if err != nil {
				t.Fatal(err)
			}
			c := rv64NativeTailCompile(t, "host-certificate-mixed", rv64MixedHostCertificateModule(), artifact)
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			owned, err := rt.NewHostFuncRef(func(v int32) int32 { return v + 2 }, c.importFuncSigs[2])
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := owned.Close(); err != nil {
					t.Error("mixed host owner cleanup", err)
				}
			})
			in := rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "native", export).Function("env", "go", func(v int32) int32 { return v + 1 }).Function("env", "owned", owned))
			for i := range 3 {
				if got, err := in.Invoke("call", 40, uint64(i)); err != nil || !slices.Equal(got, []uint64{40 + uint64(i)}) {
					t.Fatal("mixed callback/native dispatch", i, got, err)
				}
			}
			if !in.retainResourceRoot() {
				t.Fatal("mixed importer root")
			}
			defer in.releaseResourceRoot()
			err = rv64GenerationLockedForTest(func() error {
				state := in.pluginState.Load().railshotInvocation.nativeImports
				wrappers, err := railshot.NewNativeTailCodeImage(rv64TailThunkCodeOwner{in})
				if err != nil {
					return err
				}
				if err := state.records[0].validate(in, 0, wrappers); err != nil {
					return err
				}
				for _, index := range []int{1, 2} {
					owner, err := rv64NewHostTailCodeOwner(in, index)
					if err != nil {
						return err
					}
					image, err := railshot.NewNativeTailCodeImage(owner)
					if err != nil {
						return err
					}
					if owner.record.offset <= 0 || index == 1 && owner.record.dispatchIndex != 1 || index == 2 && owner.record.dispatchIndex&hostFuncRefDispatchBit == 0 {
						return fmt.Errorf("mixed wrapper offset/resolver identity")
					}
					if err := owner.record.certificate.Validate(image, owner.record.offset, owner.record.dispatchIndex, 1, 1, index == 2); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			need := uint64(in.c.arenaNeedForImports(in.imports, true) + railshotNativeImportMetadataBytes(in.c, in.imports))
			if need != in.pluginState.Load().railshotInvocation.tailPolicy.metadataBase {
				t.Fatal("mixed publication charge", need)
			}
		})
	}
}

func TestRV64HostCertificateMetadataAdmission(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(fmt.Sprintf("owned%t", owned), func(t *testing.T) {
			c := rv64NativeTailCompile(t, "host-certificate-quota", rv64PublicReferenceTailModule([]wasm.ValType{wasm.I32}, true, true), false)
			callback := HostCallFunc(func(call HostCall) { call.ResultSlots()[0] = call.ParamSlots()[0] })
			makeImports := func(rt *Runtime) *Imports {
				var fn any = callback
				if owned {
					ref, err := rt.NewHostFuncRef(callback, c.importFuncSigs[0])
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = ref.Close() })
					fn = ref
				}
				return NewImports().Function("env", "target", fn)
			}
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			in := rv64HostTailInstantiate(t, rt, c, makeImports(rt))
			cost := railshotNativeImportMetadataBytes(in.c, in.imports)
			need := uint64(in.c.arenaNeedForImports(in.imports, true) + cost)
			if cost == 0 || in.pluginState.Load().railshotInvocation.tailPolicy.metadataBase != need {
				t.Fatal("host construction not charged", cost, need)
			}
			for _, limit := range []uint64{need - 1, need} {
				limited := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithMaxInstanceMetadataBytes(limit)))
				imports := makeImports(limited)
				mod, err := limited.Module(c)
				if err != nil {
					t.Fatal(err)
				}
				got, err := limited.Instantiate(context.Background(), mod, WithImports(imports))
				if limit < need {
					var quota *coreruntime.ResourceLimitError
					if got != nil || !errors.As(err, &quota) || quota.Requested != need || quota.Limit != limit {
						t.Fatal("host metadata quota", err)
					}
				} else if err != nil {
					t.Fatal("exact host metadata quota", err)
				}
				if got != nil {
					_ = got.Close()
				}
				_ = mod.Close()
				rv64HostCertificateCloseRuntime(t, limited)
			}
		})
	}
}

func TestRV64HostCertificatePublicationFailureRetry(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(fmt.Sprintf("owned%t", owned), func(t *testing.T) {
			c := rv64NativeTailCompile(t, "host-certificate-publication", rv64PublicReferenceTailModule([]wasm.ValType{wasm.I32}, true, true), false)
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			mod, err := rt.Module(c)
			if err != nil {
				t.Fatal(err)
			}
			defer mod.Close()
			var callback any = func(v int32) int32 { return v + 1 }
			var ref *HostFuncRef
			if owned {
				ref, err = rt.NewHostFuncRef(callback, c.importFuncSigs[0])
				if err != nil {
					t.Fatal(err)
				}
				defer ref.Close()
				callback = ref
			}
			imports := NewImports().Function("env", "target", callback)
			indexes := mod.Compiled().ensureCompileIndexes()
			failure := errors.New("host certificate publication and cleanup failure")
			var retained []byte
			attempts := 0
			indexes.mapThunkCode = func([]byte) ([]byte, uintptr, error) {
				attempts++
				retained = cleanupMapping(t)
				return retained, 0, failure
			}
			indexes.unmapCode = func(mem []byte) error {
				if offHeapSlicePtr(mem) == offHeapSlicePtr(retained) {
					return failure
				}
				return coreruntime.Unmap(mem)
			}
			defer func() { indexes.mapThunkCode, indexes.unmapCode = nil, nil }()
			for range 2 {
				in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
				if in != nil || !errors.Is(err, failure) || attempts != 1 {
					t.Fatal("failed host publication was callable or retried before cleanup", err, attempts)
				}
			}
			if owned {
				if len(indexes.failedMappings) != 1 {
					t.Fatal("owned failed mapping owner")
				}
				ref.mu.Lock()
				count := ref.importers
				ref.mu.Unlock()
				if count != 0 {
					t.Fatal("failed host publication retained callback importer", count)
				}
			} else {
				cache := mod.Compiled().loadValidateMemo().hostThunks[1]
				if len(cache.mem) == 0 || cache.base != 0 || cache.offsets != nil || cache.records != nil || cache.codeBytes != 0 {
					t.Fatal("failed shared image exposed construction certificate")
				}
			}
			indexes.mapThunkCode, indexes.unmapCode = nil, nil
			in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
			if err != nil {
				t.Fatal("retry host publication", err)
			}
			defer in.Close()
			if got, err := in.Invoke("call", 41, 0); err != nil || !slices.Equal(got, []uint64{42}) {
				t.Fatal("recovered host publication", got, err)
			}
			if !in.retainResourceRoot() {
				t.Fatal("retry physical root")
			}
			defer in.releaseResourceRoot()
			if err := rv64GenerationLockedForTest(func() error {
				owner, err := rv64NewHostTailCodeOwner(in, 0)
				if err != nil {
					return err
				}
				image, err := railshot.NewNativeTailCodeImage(owner)
				if err != nil {
					return err
				}
				return owner.record.certificate.Validate(image, owner.record.offset, owner.record.dispatchIndex, 1, 1, owned)
			}); err != nil {
				t.Fatal("retry retained wrong certificate", err)
			}
		})
	}
}
