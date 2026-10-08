//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	goruntime "runtime"
	"testing"
)

func TestBoundedDirectWarmPublicationAndPanic(t *testing.T) {
	for _, view := range []bool{false, true} {
		for _, binary := range []bool{false, true} {
			for _, panics := range []bool{false, true} {
				t.Run(fmt.Sprintf("view%t/binary%t/panic%t", view, binary, panics), func(t *testing.T) {
					params, body := "i32", "local.get 0 call $step call $step"
					want := uint64(9)
					if binary {
						params = "i32 i32"
						body = "local.get 0 local.get 1 call $step local.get 1 call $step"
						want = 13
					}
					c, err := Compile(goHostSegmentConfig(), watToWasm(t, fmt.Sprintf(`(module
     (import "env" "step" (func $step (param %s) (result i32)))
     (memory (export "memory") 1)
     (func (export "run") (param i32 i32) (result i32) %s))`, params, body)))
					if err != nil {
						t.Fatal(err)
					}
					defer c.Close()
					var in *Instance
					calls := 0
					sentinel := &struct{}{}
					step := func(v int32) int32 {
						calls++
						if calls == 5 {
							if _, err := in.ExportedMemory("memory"); err != nil {
								t.Fatal(err)
							}
							if in.usesIndependentExecution() {
								t.Fatal("publication retained independent execution")
							}
							if inlineWagoGrow(32) != 528 {
								t.Fatal("stack growth after publication")
							}
							goruntime.GC()
							if panics {
								panic(sentinel)
							}
						}
						return v + 1
					}
					imports := NewImports()
					if view {
						sigParams := []ValType{ValI32}
						if binary {
							sigParams = append(sigParams, ValI32)
						}
						imports.HostFunc("env", "step", func(call HostCall) {
							v := call.I32(0)
							if binary {
								v += call.I32(1)
							}
							call.SetI32(0, step(v))
						}).Params(sigParams...).Results(ValI32)
					} else if binary {
						imports.HostFunc("env", "step", func(a, b int32) int32 { return step(a + b) }).Params(ValI32, ValI32).Results(ValI32)
					} else {
						imports.HostFunc("env", "step", step).Params(ValI32).Results(ValI32)
					}
					in, err = Instantiate(c, InstantiateOptions{Imports: imports})
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					invoke := func() {
						t.Helper()
						got, err := in.Invoke("run", 7, 2)
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("invoke: %v, %v;want %d", got, err, want)
						}
					}
					invoke()
					invoke()
					if !codeProfileEnabled && !numericContextDetached(in) && in.ensurePluginState().boundedViewVersion == 0 {
						t.Fatal("warm direct context was not selected")
					}
					var recovered any
					func() { defer func() { recovered = recover() }(); invoke() }()
					if panics && recovered != sentinel {
						t.Fatalf("panic=%v", recovered)
					}
					if !panics && recovered != nil {
						t.Fatalf("unexpected panic=%v", recovered)
					}
					state := in.ensurePluginState()
					if in.invocationState.Load() != 0 || state.invocationID != 0 || state.activations.boundedID.Load() != 0 {
						t.Fatal("invocation authority retained")
					}
					invoke()
					expected := 8
					if panics {
						expected = 7
					}
					if calls != expected {
						t.Fatalf("callbacks=%d;want %d", calls, expected)
					}
				})
			}
		}
	}
}
