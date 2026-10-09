//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicReferenceFunctionImportedTailsSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, large := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-large%t", artifact, vector, large), func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rv64HostCertificateCloseRuntime(t, rt)
					nc := rv64NativeTailCompile(t, "reference-imports-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
					native := rv64NativeTokenInstantiate(t, rt, nc, NewImports())
					nf, err := native.ExportedFunc("identity")
					if err != nil {
						t.Fatal(err)
					}
					pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-import-tails-v%t-l%t", vector, large), wasmtest.ClosedReferenceImportTails(vector, large), artifact)
					hostCalls := 0
					shared := HostCallFunc(func(call HostCall) {
						hostCalls++
						if len(call.ParamSlots()) != 1 || len(call.ResultSlots()) != 1 || call.ParamSlots()[0] != 0xfffffffd {
							panic("private tail leaked reference/payload to shared transport")
						}
						runtime.GC()
						call.ResultSlots()[0] = 0xcafebeefffffffFE
					})
					owned := rv64DynamicHostBinding(t, rt, true, HostCallFunc(func(call HostCall) {
						hostCalls++
						if len(call.ParamSlots()) != 1 || len(call.ResultSlots()) != 1 || call.ParamSlots()[0] != 0xfffffffd {
							panic("private tail leaked reference/payload to owned transport")
						}
						call.ResultSlots()[0] = 0xcafebeef00000001
					}), pc.importFuncSigs[2])
					producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports().Function("env.calls", "native.fn", nf).Function("env.calls", "shared.fn", shared).Function("env.calls", "owned.fn", owned))
					row := producer.funcRefDescs[6*coreruntime.FuncRefDescBytes : 7*coreruntime.FuncRefDescBytes]
					if !railshotNonCallableFunction(producer.c, 5) || !bytes.Equal(row, make([]byte, len(row))) {
						t.Fatal("private source acquired callable descriptor")
					}
					state := &producer.pluginState.Load().railshotInvocation
					capability := binary.LittleEndian.Uint64(rv64NativeInvocationContext(producer)[abi.RV64TailCapabilityOffset:])
					if state.tailDispatcher == nil || capability == 0 {
						t.Fatal("static private-tail factory missing")
					}
					if err := native.Close(); err != nil {
						t.Fatal(err)
					}
					if err := nc.Close(); err != nil {
						t.Fatal(err)
					}
					args := []uint64{0xdeadbeeffffffffd, 0x7ff0000000000001}
					payload := []uint64{args[1]}
					if vector {
						args = append(args, 0x80000000deadbeef)
						payload = append(payload, args[2])
					}
					args = append(args, 0, 0)
					for _, choice := range []uint64{0, 1, 2} {
						for _, null := range []uint64{0, 1} {
							args[len(args)-2], args[len(args)-1] = 0xdeadbeef00000000|choice, 0xcafefeed00000000|null
							numeric := []uint64{0xfffffffd, 0xfffffffe, 1}[choice]
							want := append(slices.Clone(payload), numeric, 113)
							before := hostCalls
							got, err := producer.Invoke("run", args...)
							if err != nil || !slices.Equal(got, want) || readGlobalObject(producer.globalCells[0], ValI32) != 113 {
								t.Fatal("private source proper tail/ordinary continuation", choice, null, got, err)
							}
							if (choice == 0 && hostCalls != before) || (choice != 0 && hostCalls != before+1) {
								t.Fatal("wrong private tail target", choice, hostCalls, before)
							}
							if binary.LittleEndian.Uint64(rv64NativeInvocationContext(producer)[abi.RV64TailCapabilityOffset:]) != capability || state.tailDispatcher == nil || state.tailGenerationBudget.count != 0 {
								t.Fatal("static private-tail capability/lifetime changed")
							}
						}
					}
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					if !native.resourcesClosed {
						t.Fatal("private tail native root survived final release")
					}
					if err := owned.(*HostFuncRef).Close(); err != nil {
						t.Fatal("private tail owned callback retained", err)
					}
				})
			}
		}
	}
}
