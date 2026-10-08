//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicReferenceCallMaximumPhysicalSignature(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				n, typ := 256, wasm.I32
				if vector {
					n, typ = 128, wasm.V128
				}
				types := make([]wasm.ValType, n)
				var identity, body []byte
				for i := range types {
					types[i] = typ
					identity = append(identity, 0x20)
					identity = append(identity, wasmtest.ULEB(uint32(i))...)
					if vector {
						lo, hi := uint64(0), uint64(0)
						if i == 0 {
							lo, hi = 0x123456789abcdef0, 0x7ff0000000000001
						}
						if i == n-1 {
							lo, hi = 0xabcdef9876543210, 0xff80012300000000
						}
						body = append(body, 0xfd, 12)
						body = binary.LittleEndian.AppendUint64(body, lo)
						body = binary.LittleEndian.AppendUint64(body, hi)
					} else {
						body = append(body, 0x41, 1)
					}
				}
				identity = append(identity, 0x0b)
				body = append(body, 0xd2, 0, 0x14, 2)
				for i := 1; i < n; i++ {
					if vector {
						body = append(body, 0xfd, 0x51)
					} else {
						body = append(body, 0x6a)
					}
				}
				body = append(body, 0x0b)
				source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, []wasm.ValType{typ}), wasmtest.FuncType(types, types))), wasmtest.Section(3, []byte{2, 0, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(identity), wasmtest.Code(body))))
				c := rv64NativeTailCompile(t, fmt.Sprintf("reference-call-max-%t", vector), source, artifact)
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				p, r, err := railshotImportedCallSlots(c, 0, 1)
				if err != nil || p != 256 || r != 256 {
					t.Fatal("declared hidden signature capacities", p, r, err)
				}
				want := []uint64{256}
				if vector {
					want = []uint64{0x123456789abcdef0 ^ 0xabcdef9876543210, 0x7ff0000000000001 ^ 0xff80012300000000}
				}
				fn, err := in.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				session, err := fn.OpenSession()
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				for i := 0; i < 3; i++ {
					got, err := session.Invoke()
					if err != nil || !slices.Equal(got, want) {
						t.Fatal("full physical call_ref transport/continuation", got, want, err)
					}
				}
				if state := &in.pluginState.Load().railshotInvocation; state.tailDispatcher != nil || state.tailGenerationBudget.count != 0 || rv64EntryCapability(in) != 0 {
					t.Fatal("ordinary call_ref retained a tail factory")
				}
			})
		}
	}
}

func TestRV64PublicReferenceCallDeadAndNullSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, dead := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-dead%t", artifact, dead), func(t *testing.T) {
				body := []byte{0xd0, 0, 0x14, 0, 0x0b}
				if dead {
					body = append([]byte{0x00}, body...)
				}
				source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
				c := rv64NativeTailCompile(t, fmt.Sprintf("reference-call-dead-%t", dead), source, artifact)
				if !c.nativeReferenceCallABI || rv64NativeTailModule(c) != nil {
					t.Fatal("ordinary reference source marker")
				}
				if !artifact && !c.nativeReferenceControlABI {
					t.Fatal("source indexed null must also retain its control witness")
				}
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if len(in.funcRefDescs) != 40 {
					t.Fatal("null-only reference added an indexed descriptor directory", len(in.funcRefDescs))
				}
				_, err = in.Invoke("run")
				var trap *coreruntime.TrapError
				want := coreruntime.TrapIndirectOutOfBounds
				if dead {
					want = coreruntime.TrapUnreachable
				}
				if !errors.As(err, &trap) || trap.Code != want {
					t.Fatal("dead/null typed reference trap", err)
				}
			})
		}
	}
}

func TestRV64PublicReferenceCallMarkerAdmission(t *testing.T) {
	// Isolate the ordinary-call witness. An indexed null now legitimately adds
	// its own retained-control witness, which also admits the typed feature.
	// Metadata validation does not reconstruct opcodes from trusted native code.
	sig := []wasm.ValType{wasm.I32}
	source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(sig, sig))), wasmtest.Section(3, []byte{2, 0, 0}), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0xd2, 0, 0x14, 0, 0x0b}))))
	base := rv64NativeTailCompile(t, "reference-call-marker", source, false)
	if !base.nativeReferenceCallABI || base.nativeReferenceControlABI || rv64NativeTailModule(base) != nil {
		t.Fatal("ordinary-call-only marker fixture")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Compiled)
	}{
		{"missing-marker", func(c *Compiled) { c.nativeReferenceCallABI = false }},
		{"missing-typed", func(c *Compiled) { c.requiredFeatures &^= CoreFeatureTypedFunctionReferences }},
		{"missing-reference-abi", func(c *Compiled) { c.nativeTableReferenceABI = false }},
		{"missing-context-abi", func(c *Compiled) { c.nativeTableContextABI = false }},
		{"missing-descriptor-extent", func(c *Compiled) { c.FuncTypeID = nil }},
		{"reference-signature", func(c *Compiled) { c.Types[0].Params[0] = ValueTypeDescriptor{Kind: ValueTypeReference} }},
		{"nonfinal-type", func(c *Compiled) { c.Types[0].Final = false }},
		{"subtype", func(c *Compiled) { c.Types[0].Supers = []uint32{1} }},
		{"ref-test", func(c *Compiled) { c.codeCache.flags |= compiledCacheDynamicFuncRefTest }},
		{"unknown-cpu-bit", func(c *Compiled) { c.requiredAMD64Features = shared.AMD64Features(1 << 18) }},
		{"cpu-marker-alias", func(c *Compiled) { c.requiredAMD64Features = shared.AMD64Features(1 << 19) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cloneCompiledMetadata(base)
			c.validateMemo, c.codeCache = &validateMemo{}, nil
			c.ensureCodeCache()
			c.codeCache.stagedFeatures = CoreFeatureTypedFunctionReferences
			tc.mutate(c)
			if err := railshotValidateCompiled(c); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("invalid call_ref shape accepted", err)
			}
			blob, err := marshalCompiled(c)
			if err != nil {
				return
			} // Structural writer validation is also a rejection.
			got, err := LoadTrustedArtifact(blob)
			if err == nil {
				if got != nil {
					_ = got.Close()
				}
				t.Fatal("loader accepted invalid call_ref marker contract")
			}
		})
	}
	if a := testing.AllocsPerRun(5, func() {
		if err := railshotValidateReferenceCalls(base); err != nil {
			panic(err)
		}
	}); a != 0 {
		t.Fatal("marker admission copied declared signatures", a)
	}
	if c, err := compatibilityDefaultConfig().Compile(rv64PublicReferenceTailModule([]wasm.ValType{wasm.I32}, false, true)); c != nil || err == nil {
		t.Fatal("default typed-reference features opened", err)
	}
}

// A marker-only producer must enter its owner scope through ordinary native
// imports and pure import reexports, even when the public root has no marker.
func TestRV64PublicReferenceCallNativeEntryGraph(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, route := range []string{"ordinary", "static-tail", "reexport"} {
			t.Run(fmt.Sprintf("artifact%t-%s", artifact, route), func(t *testing.T) {
				pc := rv64NativeTailCompile(t, "reference-call-entry-producer", rv64PublicReferenceTailModule([]wasm.ValType{wasm.I32}, false, true), artifact)
				producer, err := Instantiate(pc)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				export, err := producer.ExportedFunc("call")
				if err != nil {
					t.Fatal(err)
				}
				params, results := []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}
				source := rv64OrdinaryNativeCallerModule(params, results, route == "static-tail")
				if route == "reexport" {
					im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
					source = wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))), wasmtest.Section(2, wasmtest.Vec(im)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0))))
				}
				c := rv64NativeTailCompile(t, "reference-call-entry-"+route, source, artifact)
				in, err := Instantiate(c, NewImports().Function("env", "target", export))
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if route == "reexport" {
					if !in.retainResourceRoot() {
						t.Fatal("relay physical owner")
					}
					view, viewErr := (rv64TailCodeOwner{in}).nativeOwnerCodeView(true)
					_, strictErr := (rv64TailCodeOwner{in}).NativeTailCodeView()
					in.releaseResourceRoot()
					if viewErr != nil || len(view) != 0 || !errors.Is(strictErr, railshot.ErrUnsupported) {
						t.Fatal("empty relay is an owner, never a guest target", viewErr, strictErr)
					}
					if err := c.Close(); err != nil {
						t.Fatal(err)
					}
					if err := in.c.Close(); err != nil {
						t.Fatal(err)
					}
				}
				if c.nativeReferenceCallABI || producer.c.nativeReferenceCallABI == false {
					t.Fatal("marker-only callee fixture")
				}
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := pc.Close(); err != nil {
					t.Fatal(err)
				}

				initialCapability := map[*Instance]uint64{in: rv64EntryCapability(in), producer: rv64EntryCapability(producer)}
				invoke := func(args ...uint64) ([]uint64, error) { return in.Invoke("call", args...) }
				if route != "reexport" {
					fn, err := in.WasmFunc("call")
					if err != nil {
						t.Fatal(err)
					}
					session, err := fn.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					defer session.Close()
					invoke = func(args ...uint64) ([]uint64, error) { return session.Invoke(args...) }
				}
				for _, selector := range []uint64{0, 1, 0} {
					got, err := invoke(0xcafe123480000001, selector)
					if selector == 0 {
						if err != nil || !slices.Equal(got, []uint64{0x80000001}) {
							t.Fatal("reference callee continuation", got, err)
						}
					} else {
						var trap *coreruntime.TrapError
						if !errors.As(err, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds {
							t.Fatal("reference callee null trap", err)
						}
					}
				}
				for _, owner := range []*Instance{in, producer} {
					state := &owner.pluginState.Load().railshotInvocation
					if state.tableDepth != 0 || state.tailGenerationBudget.count != 0 || rv64EntryCapability(owner) != initialCapability[owner] {
						t.Fatal("reference owner scope leaked", state.tableDepth, state.tailGenerationBudget.count, rv64EntryCapability(owner), initialCapability[owner])
					}
				}
			})
		}
	}
}

func TestRV64PublicReferenceCallHostDescriptors(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, owned := range []bool{false, true} {
			for _, shape := range []string{"zero", "mixed"} {
				t.Run(fmt.Sprintf("artifact%t-owned%t-%s", artifact, owned, shape), func(t *testing.T) {
					var types []wasm.ValType
					var input, want []uint64
					if shape != "zero" {
						types = []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
						input = []uint64{0xcafe123480000001, 0xcafe12347f800123, 0xfedcba9876543210, 0x7ff0000000000001}
						want = []uint64{0x80000001, 0x7f800123, input[2], input[3]}
					}
					c := rv64NativeTailCompile(t, "reference-call-host-"+shape, rv64PublicReferenceTailModule(types, true, true), artifact)
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rt.Close()
					var in *Instance
					calls, fail := 0, false
					stopped := errors.New("ordinary reference host trap")
					callback := CallerHostCallFunc(func(caller Caller, call HostCall) {
						calls++
						if !caller.valid() || caller.in != in || !slices.Equal(call.ParamSlots(), want) {
							panic("reference host caller/transport")
						}
						state := &in.pluginState.Load().railshotInvocation
						if state.tableDepth == 0 || state.tailGenerationBudget.count != 0 || rv64EntryCapability(in) != 0 {
							panic("ordinary reference host owner scope")
						}
						view := rv64InvocationViewForTest(in)
						before := bytes.Clone(view)
						got, err := in.InvokeFromHost(context.Background(), caller, "identity", input...)
						if err != nil || !slices.Equal(got, want) || !bytes.Equal(view, before) {
							panic(fmt.Sprintf("reference host reentry: %v %v", got, err))
						}
						runtime.GC()
						if fail {
							panic(HostTrap{Err: stopped})
						}
						copy(call.ResultSlots(), call.ParamSlots())
					})
					var binding any = callback
					if owned {
						ref, err := rt.NewHostFuncRef(callback, c.importFuncSigs[0])
						if err != nil {
							t.Fatal(err)
						}
						defer ref.Close()
						binding = ref
					}
					in = rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "target", binding))
					if !in.syncMode {
						t.Fatal("reference host requires synchronous continuation")
					}
					args := append(slices.Clone(input), 0)
					for i := 0; i < 3; i++ {
						fail = false
						if got, err := in.Invoke("call", args...); err != nil || !slices.Equal(got, want) {
							t.Fatal("reference host return", got, err)
						}
						fail = true
						if _, err := in.Invoke("call", args...); !errors.Is(err, stopped) {
							t.Fatal("reference host trap", err)
						}
						state := &in.pluginState.Load().railshotInvocation
						if state.tableDepth != 0 || state.tailDispatcher != nil {
							t.Fatal("reference host cold cleanup")
						}
					}
					if calls != 6 {
						t.Fatal("reference host callback count", calls)
					}
				})
			}
		}
	}
}

func TestRV64PublicReferenceCallHostVoidDirections(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, noargs := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-noargs%t", artifact, noargs), func(t *testing.T) {
				params, results := []wasm.ValType{wasm.I32}, []wasm.ValType(nil)
				body := []byte{0x20, 0}
				args, want := []uint64{0xcafe123480000001}, []uint64(nil)
				if noargs {
					params, results, body = nil, params, nil
					args, want = nil, []uint64{17}
				}
				body = append(body, 0xd2, 0, 0x14, 1, 0x0b)
				im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
				source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results), wasmtest.FuncType(params, results))), wasmtest.Section(2, wasmtest.Vec(im)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
				c := rv64NativeTailCompile(t, fmt.Sprintf("reference-call-host-noargs%t", noargs), source, artifact)
				calls := 0
				in, err := Instantiate(c, NewImports().Function("env", "target", HostCallFunc(func(call HostCall) {
					calls++
					if noargs {
						if len(call.ParamSlots()) != 0 {
							panic("zero args")
						}
						call.SetI32(0, 17)
					} else if len(call.ResultSlots()) != 0 || call.I32(0) != -2147483647 {
						panic("zero results/narrow normalization")
					}
				})))
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if got, err := in.Invoke("call", args...); err != nil || !slices.Equal(got, want) || calls != 1 {
					t.Fatal("asymmetric reference host transfer", got, err, calls)
				}
				if !noargs {
					event, err := Instantiate(c, NewImports().Function("env", "target", I32HostEvent(func(int32) { t.Error("unadmitted event delivered") })))
					if event != nil {
						event.Close()
					}
					if err == nil {
						t.Fatal("reference profile admitted deferred event host")
					}
				}
			})
		}
	}
}

func TestRV64PublicReferenceCallDynamicTailContinuation(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				key := fmt.Sprintf("reference-call-dynamic-continuation-%t", vector)
				pc := rv64NativeTailCompile(t, key+"-producer", rv64PublicTailCountdown(vector, true), artifact)
				producer, err := Instantiate(pc)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				export, err := producer.ExportedFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				payload := []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
				if vector {
					payload = append(payload, wasm.V128)
				}
				params := append([]wasm.ValType{wasm.I64}, payload...)
				var body []byte
				for i := range params {
					body = append(body, 0x20, byte(i))
				}
				body = append(body, 0xd2, 0, 0x14, 1, 0x0b)
				im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
				source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, payload), wasmtest.FuncType(params, payload))), wasmtest.Section(2, wasmtest.Vec(im)), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))), wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
				c := rv64NativeTailCompile(t, key+"-caller", source, artifact)
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithNativeStackBytes(MinNativeStackBytes)))
				defer rt.Close()
				in := rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "target", export))
				if err := producer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := pc.Close(); err != nil {
					t.Fatal(err)
				}
				input := []uint64{2049, 0xcafe123480000001, 0xcafe12347f800123, 0xfedcba9876543210, 0x7ff0000000000001}
				want := []uint64{0x80000001, 0x7f800123, input[3], input[4]}
				if vector {
					input = append(input, 0xaabbccddeeff0011, 0xffeeccbb00112233)
					want = append(want, input[5:]...)
				}
				fn, err := in.WasmFunc("call")
				if err != nil {
					t.Fatal(err)
				}
				session, err := fn.OpenSession()
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				for range 3 {
					if got, err := session.Invoke(input...); err != nil || !slices.Equal(got, want) {
						t.Fatal("ordinary reference continuation after proper tails", got, err)
					}
				}
				for _, owner := range []*Instance{in, producer} {
					state := &owner.pluginState.Load().railshotInvocation
					if state.tableDepth != 0 || state.tailGenerationBudget.count != 0 || rv64EntryCapability(owner) != 0 {
						t.Fatal("ordinary reference tail continuation leaked scope")
					}
				}
			})
		}
	}
}
