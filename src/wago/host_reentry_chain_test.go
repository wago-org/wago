//go:build !tinygo

package wago

import (
	"context"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestHostReentryAllowsComponentAdapterChain(t *testing.T) {
	// A reactor can cross more than four adapter boundaries without recursion.
	const adapters = 8
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x60, 0, 0})),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "next", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))),
	)
	compiled, err := Compile(nil, module)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instances := make([]*Instance, adapters)
	defer func() {
		for _, in := range instances {
			if in != nil {
				in.Close()
			}
		}
	}()
	calls := 0
	for index := adapters - 1; index >= 0; index-- {
		index := index
		host := CallerHostCallFunc(func(caller Caller, _ HostCall) {
			calls++
			if index+1 < len(instances) {
				if _, err := instances[index+1].InvokeFromHost(context.Background(), caller, "call"); err != nil {
					panic(HostTrap{Err: err})
				}
			}
		})
		instances[index], err = Instantiate(compiled, InstantiateOptions{Imports: testImports("env.next", host)})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := instances[0].Invoke("call"); err != nil {
		t.Fatal(err)
	}
	if calls != adapters {
		t.Fatalf("adapter calls = %d, want %d", calls, adapters)
	}
	assertHostReentryDepthsEmpty(t)
}
