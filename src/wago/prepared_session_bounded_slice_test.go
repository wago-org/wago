//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"testing"
)

func TestPreparedSessionBoundedSliceWidthsAndPanic(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains scalar/general entry")
	}
	for _, kind := range []string{"typed", "HostCall", "Caller"} {
		t.Run(kind, func(t *testing.T) {
			valueType, narrow := "i64", ""
			if kind == "typed" {
				valueType, narrow = "i32", "i32.wrap_i64"
			}
			c, err := Compile(goHostSegmentConfig(), watToWasm(t, fmt.Sprintf(`(module
 (import "env" "step" (func $step (param %[1]s) (result %[1]s)))
 (func (export "run") (param i32 i64 i32 i64 i32) (result %[1]s)
  local.get 0 i64.extend_i32_s local.get 1 i64.add
  local.get 2 i64.extend_i32_s i64.add local.get 3 i64.add
  local.get 4 i64.extend_i32_s i64.add %[2]s call $step))`, valueType, narrow)))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			calls, fail := 0, false
			sentinel := &struct{}{}
			var stale Caller
			step := func(v int64) int64 {
				calls++
				if fail {
					panic(sentinel)
				}
				return v + 1
			}
			var callback any = func(v int32) int32 { return int32(step(int64(v))) }
			valType, want := ValI32, uint64(3)
			if kind != "typed" {
				valType, want = ValI64, uint64(1<<40)+3
				callback = func(call HostCall) { call.SetI64(0, step(call.I64(0))) }
				if kind == "Caller" {
					callback = func(caller Caller, call HostCall) {
						if !caller.valid() || stale.valid() {
							t.Fatal("Caller generation")
						}
						stale = caller
						call.SetI64(0, step(call.I64(0)))
					}
				}
			}
			imports := NewImports()
			imports.HostFunc("env", "step", callback).Params(valType).Results(valType)
			in, err := Instantiate(c, InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			if !fn.boundedNumericHost {
				t.Fatal("slice fixture did not select bounded numeric entry")
			}
			session, err := fn.OpenSession()
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			state := in.ensurePluginState()
			reservedID := state.invocationID
			args := []uint64{0x12345678ffffffff, I64(1 << 40), 0xabcdef0000000002, I64(-3), 0x1111222200000004}
			check := func() {
				t.Helper()
				got, err := session.Invoke(args...)
				if err != nil || len(got) != 1 || got[0] != want || stale.valid() {
					t.Fatalf("result %v, %v; want %d", got, err, want)
				}
			}
			check()
			check()
			fail = true
			var recovered any
			func() { defer func() { recovered = recover() }(); _, _ = session.Invoke(args...) }()
			if recovered != sentinel || stale.valid() || state.invocationID != reservedID || session.state.active.Load() || state.activations.boundedID.Load() != 0 {
				t.Fatal("slice panic changed reservation or retained callback authority")
			}
			fail = false
			check()
			if calls != 4 {
				t.Fatal("callback count", calls)
			}
		})
	}
}
