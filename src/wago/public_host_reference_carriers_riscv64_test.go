//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PublicHostReferenceCarrierModule(tail ...bool) []byte {
	types := []wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.I64, wasm.F32, wasm.F64}
	im := append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)
	opcode := byte(0x10)
	if len(tail) != 0 && tail[0] {
		opcode = 0x12
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))),
		wasmtest.Section(2, wasmtest.Vec(im)),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x20, 4, 0x20, 5, opcode, 0, 0x0b}))),
	)
}

func TestRV64PublicHostReferenceCarriersSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, tail := range []bool{false, true} {
			for _, owned := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-tail%t-owned%t", artifact, tail, owned), func(t *testing.T) {
					if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
						t.Skip("precompiled artifact path only")
					}
					wide := int64(-81985529216486896)
					f64 := uint64(0xfff8000000000123)
					key := fmt.Sprintf("host-reference-carriers-tail%t", tail)
					c := rv64NativeTailCompile(t, key, rv64PublicHostReferenceCarrierModule(tail), artifact)
					m := rv64NativeTailModule(c)
					if !rv64PublicHostReferenceBoundary(c) || !c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || c.nativeReferenceControlABI || !tail && m != nil || tail && (m == nil || len(m.Plans) != 1 || m.Plans[0].Kind != encoder.NativeTailDirectImport || !rv64PublicHostReferenceTailPlan(c, m.Plans[0])) {
						t.Fatalf("public host reference carrier metadata: flags=%t/%t/%t imports=%d:%v sigs=%+v funcs=%+v types=%+v values=%+v exports=%v features=%#x amd=%#x escape=%t desc=%t header=%t ids=%v entry=%v internal=%v names=%v", c.nativeReferenceFunctionABI, c.nativeReferenceCallABI, c.nativeReferenceControlABI, c.NumImports, c.Imports, c.importFuncSigs, c.Funcs, c.Types, c.ValueTypes, c.Exports, c.requiredFeatures, c.requiredAMD64Features, c.dynamicFuncrefEscape, c.NeedsFuncRefDescs, c.needsFuncRefContextHeader, c.FuncTypeID, c.Entry, c.InternalEntry, c.Names)
					}
					if !rv64PublicHostReferenceBoundary(c.executionView()) {
						t.Fatal("public host reference carrier execution snapshot")
					}
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rv64HostCertificateCloseRuntime(t, rt)
					ref, err := rt.NewExternRef("host-reference-carrier")
					if err != nil {
						t.Fatal(err)
					}
					producerCode := rv64NativeTailCompile(t, "host-reference-carrier-producer", rv64PublicReferenceCarrierModule(false), artifact)
					producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
					defer producer.Close()
					callable, err := producer.Invoke("get")
					if err != nil || len(callable) != 1 || callable[0] == 0 {
						t.Fatal("host reference argument producer", callable, err)
					}
					descriptor, ok := rt.refStore.resolve(callable[0])
					if !ok || descriptor == 0 {
						t.Fatal("host reference argument descriptor")
					}
					calls := 0
					host := HostCallFunc(func(call HostCall) {
						calls++
						if calls == 1 {
							if uint32(call.I32(0)) != 0x80000001 || call.FuncRef(1).IsNull() || call.ExternRef(2) != ref || call.I64(3) != wide || call.ParamSlots()[4] != 0x7f800123 || call.ParamSlots()[5] != f64 {
								panic("host reference carrier arguments")
							}
						} else if call.I32(0) != 7 || !call.FuncRef(1).IsNull() || !call.ExternRef(2).IsNull() || call.I64(3) != 9 || call.ParamSlots()[4] != 11 || call.ParamSlots()[5] != 13 {
							panic("null host reference carrier arguments")
						}
						call.ResultSlots()[0] = 0xeeeeeeee00000000 | uint64(uint32(call.I32(0)))
						call.SetFuncRef(1, call.FuncRef(1))
						call.SetExternRef(2, call.ExternRef(2))
						call.SetI64(3, call.I64(3))
						call.ResultSlots()[4] = 0xdddddddd00000000 | call.ParamSlots()[4]
						call.ResultSlots()[5] = call.ParamSlots()[5]
					})
					binding := any(host)
					if owned {
						owner, err := rt.NewHostFuncRef(host, c.importFuncSigs[0])
						if err != nil {
							t.Fatal(err)
						}
						defer owner.Close()
						binding = owner
					}
					in := rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "target", binding))
					capability := rv64EntryCapability(in)
					if tail != (capability != 0) {
						t.Fatal("host reference carrier static tail capability", capability)
					}
					if err := in.beginInvocation(); err != nil {
						t.Fatal(err)
					}
					counts, metadata, ownerErr := rv64NativeTailOwnerCounts(in)
					in.endInvocation()
					if ownerErr != nil || counts.Functions != 0 || counts.Owners != 0 || metadata != 0 {
						t.Fatal("host reference carrier entered dynamic owner snapshots", counts, metadata, ownerErr)
					}
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					if err := producerCode.Close(); err != nil {
						t.Fatal(err)
					}
					got, err := in.Invoke("call", 0xfeedface80000001, callable[0], ValueExternRef(ref).Bits(), uint64(wide), 0xcafe12347f800123, f64)
					returned, returnedOK := uint64(0), false
					if len(got) == 6 {
						returned, returnedOK = rt.refStore.resolve(got[1])
					}
					if err != nil || len(got) != 6 || got[0] != 0x80000001 || got[2] != ValueExternRef(ref).Bits() || got[3] != uint64(wide) || got[4] != 0x7f800123 || got[5] != f64 || !returnedOK || returned != descriptor || calls != 1 {
						t.Fatal("host reference carrier transport", got, calls, err)
					}
					if got, err = in.Invoke("call", 0xcafe123400000007, 0, 0, 9, 0xeeeeeeee0000000b, 13); err != nil || !slices.Equal(got, []uint64{7, 0, 0, 9, 11, 13}) || calls != 2 {
						t.Fatal("null host reference carrier transport", got, calls, err)
					}
					rv64AssertPublicReferenceCarrierStateClear(t, in, capability)
					if !rt.ReleaseExternRef(ref) {
						t.Fatal("host reference carrier externref remained pinned")
					}
				})
			}
		}
	}
}

func TestRV64PublicHostReferenceCarrierInvalidResultsAndReuse(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, tail := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-tail%t", artifact, tail), func(t *testing.T) {
				if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
					t.Skip("precompiled artifact path only")
				}
				c := rv64NativeTailCompile(t, fmt.Sprintf("host-reference-invalid-results-tail%t", tail), rv64PublicHostReferenceCarrierModule(tail), artifact)
				if !rv64PublicHostReferenceBoundary(c.executionView()) {
					t.Fatal("public host reference carrier execution snapshot")
				}
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rv64HostCertificateCloseRuntime(t, rt)
				bad, calls := 0, 0
				host := HostCallFunc(func(call HostCall) {
					calls++
					copy(call.ResultSlots(), call.ParamSlots())
					if bad != 0 {
						call.ResultSlots()[bad] = ^uint64(0)
					}
				})
				in := rv64HostTailInstantiate(t, rt, c, NewImports().Function("env", "target", host))
				capability := rv64EntryCapability(in)
				if tail != (capability != 0) {
					t.Fatal("host reference carrier static tail capability", capability)
				}
				args := []uint64{0xcafe123400000007, 0, 0, 9, 0xeeeeeeee0000000b, 13}
				for _, invalid := range []int{1, 2} {
					bad = invalid
					if got, err := in.Invoke("call", args...); err == nil || len(got) != 0 {
						t.Fatal("invalid host reference result accepted", invalid, got, err)
					}
					rv64AssertPublicReferenceCarrierStateClear(t, in, capability)
				}
				bad = 0
				if got, err := in.Invoke("call", args...); err != nil || !slices.Equal(got, []uint64{7, 0, 0, 9, 11, 13}) || calls != 3 {
					t.Fatal("host reference bridge reuse", got, calls, err)
				}
				rv64AssertPublicReferenceCarrierStateClear(t, in, capability)
			})
		}
	}
}
