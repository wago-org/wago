//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// Integer locals spanning the pin bank must survive arbitrary Go execution,
// stack growth, GC and same-instance reentry through the private integer bridge.
func TestIntegerHostContextPreservesPinnedLocals(t *testing.T) {
	var decl, init, innerInit, update, sum strings.Builder
	for i := 0; i < 8; i++ {
		fmt.Fprintf(&decl, "(local $i%d i64) ", i)
		fmt.Fprintf(&init, "i64.const %d local.set $i%d ", i+1, i)
		fmt.Fprintf(&innerInit, "i64.const %d local.get 0 i64.extend_i32_u i64.add local.set $i%d ", i+1, i)
		fmt.Fprintf(&update, "local.get $i%d i64.const %d i64.add local.set $i%d ", i, i+1, i)
		fmt.Fprintf(&sum, "local.get $i%d i64.add ", i)
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
					runtime.GC()
					return v + 1
				}
				var host any = step
				if kind == "HostCall" {
					host = func(call HostCall) { call.SetI32(0, step(call.I32(0))) }
				}
				if kind == "Caller" {
					host = func(caller Caller, call HostCall) {
						got, err := in.InvokeFromHost(context.Background(), caller, "inner", I32(call.I32(0)))
						if err != nil || len(got) != 1 || got[0] != 36+uint64(call.I32(0))*8 {
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
				p := in.eng.PreparedScalarHost()
				if p == nil {
					t.Skip("prepared bridge unavailable")
				}
				if !codeProfileEnabled && detachedNumericHostEnabled && integerNumericHostEnabled && (runtime.GOARCH == "amd64" || armIntegerNumericEnabled) && !p.IntegerGuestContext() {
					t.Fatal("private integer bridge not selected")
				}
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
					if err != nil || len(got) != 1 || got[0] != 36+n*36 {
						t.Fatalf("n=%d got %v, %v; want %d", n, got, err, 36+n*36)
					}
				}
				if calls != 11 {
					t.Fatal("callback count", calls)
				}
			})
		}
	}
}

// The private view bridge consumes the import's validated, immutable shape.
// Exercise empty and wide imports, not just the one-slot latency fixture.
func TestIntegerHostContextViewSlotShapes(t *testing.T) {
	for _, n := range []int{0, 1, 2, 8, 16, 32, 64} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			var params, results, body strings.Builder
			sig := make([]ValType, n)
			for i := range sig {
				sig[i] = ValI32
				params.WriteString("i32 ")
				results.WriteString("i32 ")
				if n <= 16 {
					fmt.Fprintf(&body, "local.get 0 i32.const %d i32.add ", i)
				} else {
					body.WriteString("local.get 0 ")
				}
			}
			body.WriteString("call $step ")
			if n == 0 {
				body.WriteString("i32.const 7")
			} else {
				for i := 1; i < n; i++ {
					body.WriteString("i32.add ")
				}
			}
			module := fmt.Sprintf(`(module
    (import "env" "step" (func $step (param %s) (result %s)))
    (func (export "run") (param i32) (result i32) %s))`, params.String(), results.String(), body.String())
			c, err := Compile(goHostSegmentConfig(), watToWasm(t, module))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			for _, kind := range []string{"HostCall", "Caller"} {
				calls := 0
				step := func(call HostCall) {
					calls++
					for i := 0; i < n; i++ {
						call.SetI32(i, 2*call.I32(i)+int32(i)+1)
					}
				}
				var host any = step
				if kind == "Caller" {
					host = func(caller Caller, call HostCall) {
						if !caller.valid() {
							panic("expired Caller")
						}
						step(call)
					}
				}
				imports := NewImports()
				imports.HostFunc("env", "step", host).Params(sig...).Results(sig...)
				in, err := Instantiate(c, InstantiateOptions{Imports: imports})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				p := in.eng.PreparedScalarHost()
				if p == nil {
					t.Skip("prepared bridge unavailable")
				}
				if !codeProfileEnabled && detachedNumericHostEnabled && integerNumericHostEnabled && (runtime.GOARCH == "amd64" || armIntegerNumericEnabled) && !p.IntegerGuestContext() {
					t.Fatal("private integer bridge not selected")
				}
				fn, err := in.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				for _, invoke := range []func(uint64) ([]uint64, error){
					func(v uint64) ([]uint64, error) { return in.Invoke("run", v) },
					func(v uint64) ([]uint64, error) { return fn.Invoke(v) },
					func(v uint64) ([]uint64, error) {
						s, err := fn.OpenSession()
						if err != nil {
							return nil, err
						}
						defer s.Close()
						return s.Invoke1(v)
					},
				} {
					for _, v := range []uint64{0, 3, 11} {
						got, err := invoke(v)
						want := uint64(2*n)*v + uint64(n*(n+1)/2)
						if n <= 16 {
							want += uint64(n * (n - 1))
						}
						if n == 0 {
							want = 7
						}
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("%s v=%d: %v %v; want %d", kind, v, got, err, want)
						}
					}
				}
				if calls != 9 {
					t.Fatal("callback count", calls)
				}
			}
		})
	}
}
