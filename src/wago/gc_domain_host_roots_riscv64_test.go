//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64DomainHostRootModule() []byte {
	callback := append(append(wasmtest.Name("env"), wasmtest.Name("callback")...), 0, 1)
	body := []byte{
		1, 1, 0x63, 0, // one local of nullable struct-reference type zero
		0x20, 0, 0xfb, 0, 0, 0x21, 1, // struct.new 0; local.set 1
		0x10, 0, // call callback
		0x20, 1, 0xfb, 2, 0, 0, // local.get 1; struct.get 0 0
		0x0b,
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x5f, 1, 0x7f, 0},
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(callback)),
		wasmtest.Section(3, []byte{1, 2}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))),
	)
}

func TestRV64GCDomainHostRootsAcrossReentry(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact=%t", artifact), func(t *testing.T) {
			runtime := NewRuntime()
			defer runtime.Close()
			compiled, err := Compile(rv64DomainHostRootModule())
			if err != nil {
				t.Fatal(err)
			}
			if artifact {
				blob, err := compiled.MarshalBinary()
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := LoadTrustedArtifact(blob)
				if err != nil {
					t.Fatal(err)
				}
				if err := compiled.Close(); err != nil {
					t.Fatal(err)
				}
				compiled = loaded
			}
			defer compiled.Close()
			module, err := runtime.Module(compiled)
			if err != nil {
				t.Fatal(err)
			}
			defer module.Close()
			config := GCConfig{
				CollectEveryAlloc:    true,
				StressNurseryBytes:   64,
				ForceMajorEveryMinor: true,
				VerifyAfterCollect:   true,
			}
			callee, err := runtime.Instantiate(
				context.Background(), module,
				WithGC(config),
				WithImports(NewImports().Function("env", "callback", CallerHostCallFunc(func(Caller, HostCall) {}))),
			)
			if err != nil {
				t.Fatal(err)
			}
			defer callee.Close()
			var nested []uint64
			caller, err := runtime.Instantiate(
				context.Background(), module,
				WithGC(config),
				WithImports(NewImports().Function("env", "callback", CallerHostCallFunc(func(host Caller, _ HostCall) {
					var invokeErr error
					nested, invokeErr = callee.InvokeFromHost(context.Background(), host, "run", 99)
					if invokeErr != nil {
						panic(invokeErr)
					}
				}))),
			)
			if err != nil {
				t.Fatal(err)
			}
			defer caller.Close()
			got, err := caller.Invoke("run", 42)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(nested, []uint64{99}) || !slices.Equal(got, []uint64{42}) {
				t.Fatal("collector lost a parked same-domain frame root", nested, got)
			}
		})
	}
}
