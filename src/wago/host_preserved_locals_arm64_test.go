//go:build (linux || darwin) && arm64 && !tinygo && go1.22 && !go1.28

package wago

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// Numeric locals spanning the GP and scalar-FP pin banks must survive arbitrary
// Go execution and nested guest entry; their frame homes may be stale when the
// synchronous bridge preserves their registers directly.
func TestHostCallPreservedNumericLocals(t *testing.T) {
	var decl, init, innerInit, update, sum strings.Builder
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&decl, "(local $i%d i64) (local $f%d f64) ", i, i)
		fmt.Fprintf(&init, "i64.const %d local.set $i%d f64.const %d local.set $f%d ", i+1, i, i+101, i)
		fmt.Fprintf(&innerInit, "i64.const %d local.get 0 i64.extend_i32_u i64.add local.set $i%d f64.const %d local.get 0 f64.convert_i32_u f64.add local.set $f%d ", i+1, i, i+101, i)
		fmt.Fprintf(&update, "local.get $i%d i64.const %d i64.add local.set $i%d local.get $f%d f64.const %d f64.add local.set $f%d ", i, i+1, i, i, i+1, i)
		fmt.Fprintf(&sum, "local.get $i%d i64.add local.get $f%d i64.trunc_f64_s i64.add ", i, i)
	}
	module := watToWasm(t, fmt.Sprintf(`(module
  (import "env" "step" (func $step (param i32) (result i32)))
  (func (export "inner") (param i32) (result i64) %s %s i64.const 0 %s)
  (func (export "run") (param $n i32) (result i64) %s %s
   block $done loop $next
    local.get $n i32.eqz br_if $done
    local.get $n call $step drop
    %s
    local.get $n i32.const 1 i32.sub local.set $n
    br $next
   end end
   i64.const 0 %s))`, decl.String(), innerInit.String(), sum.String(), decl.String(), init.String(), update.String(), sum.String()))
	c, err := Compile(goHostSegmentConfig(), module)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, kind := range []string{"typed", "HostCall", "Caller"} {
		for _, api := range []string{"instance", "prepared", "session"} {
			t.Run(kind+"/"+api, func(t *testing.T) {
				calls := 0
				var in *Instance
				step := func(v int32) int32 {
					calls++
					if inlineWagoGrow(16) != 136 {
						t.Fatal("stack growth")
					}
					return v + 1
				}
				var host any = step
				if kind == "HostCall" {
					host = func(call HostCall) { call.SetI32(0, step(call.I32(0))) }
				}
				if kind == "Caller" {
					host = func(caller Caller, call HostCall) {
						got, err := in.InvokeFromHost(context.Background(), caller, "inner", I32(call.I32(0)))
						if err != nil || len(got) != 1 || got[0] != 872+uint64(call.I32(0))*16 {
							t.Fatalf("reentry %v, %v", got, err)
						}
						call.SetI32(0, step(call.I32(0)))
					}
				}
				imports := NewImports()
				imports.HostFunc("env", "step", host).Params(ValI32).Results(ValI32)
				in, err = Instantiate(c, InstantiateOptions{Imports: imports})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				fn, err := in.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				invoke := func(n uint64) ([]uint64, error) { return in.Invoke("run", n) }
				if api == "prepared" {
					invoke = func(n uint64) ([]uint64, error) { return fn.Invoke(n) }
				}
				if api == "session" {
					s, err := fn.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					defer s.Close()
					invoke = func(n uint64) ([]uint64, error) { return s.Invoke1(n) }
				}
				for _, n := range []uint64{0, 1, 3, 7} {
					got, err := invoke(n)
					if err != nil || len(got) != 1 || got[0] != 872+n*72 {
						t.Fatalf("n=%d got %v, %v; want %d", n, got, err, 872+n*72)
					}
				}
				if calls != 11 {
					t.Fatal("callback count", calls)
				}
			})
		}
	}
}
