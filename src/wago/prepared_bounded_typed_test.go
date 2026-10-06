//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	goruntime "runtime"
	"testing"
)

func TestPreparedBoundedTypedBinaryPublicationAndPanic(t *testing.T) {
	c, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
 (import "env" "step" (func $step (param i32 i32) (result i32)))
 (memory (export "memory") 1)
 (func (export "run") (param i32 i32) (result i32)
  local.get 0 local.get 1 call $step))`))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, api := range []string{"prepared", "session"} {
		t.Run(api, func(t *testing.T) {
			var in *Instance
			calls := 0
			sentinel := &struct{}{}
			imports := NewImports()
			imports.HostFunc("env", "step", func(a, b int32) int32 {
				calls++
				if calls == 3 {
					if _, err := in.ExportedMemory("memory"); err != nil {
						t.Fatal(err)
					}
					if inlineWagoGrow(32) != 528 {
						t.Fatal("stack growth")
					}
					goruntime.GC()
					panic(sentinel)
				}
				return a + b
			}).Params(ValI32, ValI32).Results(ValI32)
			in, err = Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			invoke := func() ([]uint64, error) { return fn.Invoke(I32(-1), I32(2)) }
			if api == "session" {
				session, err := fn.OpenSession()
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				invoke = func() ([]uint64, error) { return session.Invoke2(I32(-1), I32(2)) }
			}
			check := func() {
				t.Helper()
				got, err := invoke()
				if err != nil || len(got) != 1 || got[0] != 1 {
					t.Fatalf("result %v, %v", got, err)
				}
			}
			check()
			check()
			state := in.ensurePluginState()
			id := state.invocationID
			var recovered any
			func() { defer func() { recovered = recover() }(); _, _ = invoke() }()
			if recovered != sentinel || in.usesIndependentExecution() {
				t.Fatalf("publication/panic: %v, private %t", recovered, in.usesIndependentExecution())
			}
			if state.invocationID != id || state.activations.boundedID.Load() != 0 {
				t.Fatal("panic changed reservation or retained callback authority")
			}
			check()
			if calls != 4 {
				t.Fatal("callback count", calls)
			}
		})
	}
}
