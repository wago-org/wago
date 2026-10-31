//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func TestRV64PublicDynamicHostReferenceCarriers(t *testing.T) {
	for _, ordinary := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			for _, owned := range []bool{false, true} {
				t.Run(fmt.Sprintf("ordinary%t-artifact%t-owned%t", ordinary, artifact, owned), func(t *testing.T) {
					if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
						t.Skip("precompiled artifact path only")
					}
					types := []wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.I64, wasm.F32, wasm.F64}
					input := []uint64{0xfeedface80000001, 0, 0, 0xfedcba9876543210, 0xcafe12347f800123, 0xfff8000000000123}
					want := []uint64{0x80000001, 0, 0, input[3], 0x7f800123, input[5]}
					source := rv64PublicReferenceTailModule(types, true, ordinary)
					c := rv64NativeTailCompile(t, fmt.Sprintf("dynamic-host-reference-carriers-ordinary%t", ordinary), source, artifact)
					m := rv64NativeTailModule(c)
					if !rv64SourceContractValid(c) || ordinary && m != nil || !ordinary && (m == nil || len(m.Plans) != 2 || m.Plans[0].Kind != encoder.NativeTailReference || m.Plans[1].Kind != encoder.NativeTailReference) {
						t.Fatal("dynamic host reference retained boundary", m)
					}
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rv64HostCertificateCloseRuntime(t, rt)
					ref, err := rt.NewExternRef("dynamic-host-reference-carrier")
					if err != nil {
						t.Fatal(err)
					}
					producerCode := rv64NativeTailCompile(t, "dynamic-host-reference-carrier-producer", rv64PublicReferenceCarrierModule(false), artifact)
					producer := rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
					callable, err := producer.Invoke("get")
					if err != nil || len(callable) != 1 || callable[0] == 0 {
						t.Fatal("dynamic host reference producer", callable, err)
					}
					input[1], input[2] = callable[0], ValueExternRef(ref).Bits()
					want[1], want[2] = input[1], input[2]
					calls := 0
					host := HostCallFunc(func(call HostCall) {
						calls++
						if uint32(call.I32(0)) != uint32(want[0]) || call.FuncRef(1).IsNull() || call.ExternRef(2) != ref || call.I64(3) != int64(want[3]) || call.ParamSlots()[4] != want[4] || call.ParamSlots()[5] != want[5] {
							panic("dynamic host reference carrier arguments")
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
					if rv64StaticTailPlanCount(m) != 0 || rv64EntryCapability(in) != 0 || in.pluginState.Load().railshotInvocation.tailDispatcher != nil {
						t.Fatal("dynamic host reference acquired competing static capability")
					}
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					if err := producerCode.Close(); err != nil {
						t.Fatal(err)
					}
					got, err := in.Invoke("call", append(slices.Clone(input), 0)...)
					if err != nil || !slices.Equal(got, want) || calls != 1 {
						t.Fatal("dynamic host reference carrier transport", got, calls, err)
					}
					rv64AssertPublicReferenceCarrierStateClear(t, in)
					if got, err = in.Invoke("call", 0, 0, 0, 9, 11, 13, 1); err == nil || len(got) != 0 || calls != 1 {
						t.Fatal("dynamic null reference target did not trap", got, calls, err)
					}
					rv64AssertPublicReferenceCarrierStateClear(t, in)
					if !rt.ReleaseExternRef(ref) {
						t.Fatal("dynamic host reference externref remained pinned")
					}
				})
			}
		}
	}
}
