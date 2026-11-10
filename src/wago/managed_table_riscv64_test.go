//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64ManagedVoidTableHostAndRetiredProducer(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("foreign%t/artifact%t", foreign, artifact), func(t *testing.T) {
				rt := NewRuntime()
				defer rt.Close()
				ext := &managedTestExtension{}
				if err := rt.Use(ext); err != nil {
					t.Fatal(err)
				}
				sections := [][]byte{
					wasmtest.Section(1, []byte{1, 0x60, 0, 0}),
					wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("bump")...), 0, 0))),
				}
				function := uint32(0)
				if foreign {
					function = 1
					sections = append(sections, wasmtest.Section(3, []byte{1, 0}))
				}
				sections = append(sections,
					wasmtest.Section(4, []byte{1, 0x70, 0, 1}),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("table", 1, 0))),
					wasmtest.Section(9, wasmtest.Vec(tableTestActiveElem(0, function))),
				)
				if foreign {
					sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x10, 0, 0x0b}))))
				}
				code := rv64TableStageCompile(t, fmt.Sprintf("managed-producer-%t", foreign), wasmtest.Module(sections...), artifact)
				mod, err := rt.Module(code)
				if err != nil {
					t.Fatal(err)
				}
				calls := 0
				var expected *Instance
				imports := NewImports().Function("env", "bump", CallerHostCallFunc(func(caller Caller, _ HostCall) {
					host, valid := resolveHostCaller(caller)
					if !valid || host.in != expected {
						panic("managed table callback has the wrong caller")
					}
					calls++
				}))
				var owned *ManagedInstance
				var ordinary *Instance

				if foreign {
					producer, e := rt.Instantiate(context.Background(), mod, WithImports(imports))
					if e != nil {
						t.Fatal(e)
					}
					expected = producer
					table, e := producer.ExportedTable("table")
					if e != nil {
						t.Fatal(e)
					}
					consumerCode := rv64TableStageCompile(t, "managed-consumer", wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 0, 1)))), artifact)
					consumer, e := rt.Module(consumerCode)
					if e != nil {
						t.Fatal(e)
					}
					owned, err = ext.manager.Instantiate(context.Background(), consumer, WithImports(NewImports().Table("env", "table", table)))
					if err != nil {
						t.Fatal(err)
					}
					ordinaryCode := rv64TableStageCompile(t, "managed-ordinary-consumer", wasmtest.Module(
						wasmtest.Section(1, []byte{1, 0x60, 0, 0}),
						wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 0, 1))),
						wasmtest.Section(3, []byte{1, 0}),
						wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
						wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 0, 0x11, 0, 0, 0x0b}))),
					), artifact)
					ordinaryModule, e := rt.Module(ordinaryCode)
					if e != nil {
						t.Fatal(e)
					}
					ordinary, e = rt.Instantiate(context.Background(), ordinaryModule, WithImports(NewImports().Table("env", "table", table)))
					if e != nil {
						t.Fatal(e)
					}
					if err = producer.Close(); err != nil {
						t.Fatal(err)
					}
				} else {
					owned, err = ext.manager.Instantiate(context.Background(), mod, WithImports(imports))
					if err != nil {
						t.Fatal(err)
					}
				}
				if !foreign {
					expected = owned.Instance()
				}
				for i := 0; i < 3; i++ {
					if err := owned.InvokeVoidTable(context.Background(), 0); err != nil {
						t.Fatal(err)
					}
					if calls != i+1 {
						t.Fatalf("host calls = %d, want %d", calls, i+1)
					}
				}
				allocs := testing.AllocsPerRun(100, func() {
					if err := owned.InvokeVoidTable(context.Background(), 0); err != nil {
						panic(err)
					}
				})
				t.Logf("warm managed table allocations = %.0f", allocs)
				if !foreign && allocs != 0 {
					t.Fatalf("warm managed host-import allocations = %.0f, want 0", allocs)
				}
				if foreign {
					baseline := testing.AllocsPerRun(100, func() {
						if _, err := ordinary.Invoke("run"); err != nil {
							panic(err)
						}
					})
					t.Logf("ordinary foreign table allocations = %.0f", baseline)
					if allocs > baseline {
						t.Fatalf("managed foreign call allocations = %.0f, ordinary = %.0f", allocs, baseline)
					}
				}
				before := calls
				canceled, cancel := context.WithCancel(context.Background())
				cancel()
				if err := owned.InvokeVoidTable(canceled, 0); err != context.Canceled {
					t.Fatalf("canceled call: %v", err)
				}
				if calls != before {
					t.Fatalf("canceled call reached host: %d", calls)
				}
			})
		}
	}
}
