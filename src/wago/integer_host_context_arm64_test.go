//go:build (linux || darwin) && arm64 && !tinygo

package wago

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func TestARMIntegerHostRegisterAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, body, locals string
		want               bool
	}{
		{"integer", "local.get 0 call $step", "", true},
		{"width", "local.get 0 call $step i64.extend_i32_u i32.wrap_i64", "(local i64)", true},
		{"popcnt32", "local.get 0 i32.popcnt call $step", "", false},
		{"popcnt64", "local.get 0 i64.extend_i32_u i64.popcnt i32.wrap_i64 call $step", "", false},
		{"unused-float-local", "local.get 0 call $step", "(local f64)", false},
		{"float", "f32.const 1 f32.const 2 f32.add drop local.get 0 call $step", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Compile(goHostSegmentConfig(), watToWasm(t, fmt.Sprintf(`(module (import "env" "step" (func $step (param i32)(result i32))) (func (export "run") (param i32)(result i32) %s %s))`, tc.locals, tc.body)))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if c.integerHostContextAllowed() != tc.want {
				t.Fatalf("register grant = %t, want %t", c.integerHostContextAllowed(), tc.want)
			}
			blob, err := c.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			var decoded Compiled
			if err := decoded.UnmarshalBinary(blob); err != nil {
				t.Fatal(err)
			}
			defer decoded.Close()
			if decoded.integerHostContextAllowed() {
				t.Fatal("artifact retained compiler register grant")
			}
			for _, compiled := range []*Compiled{c, &decoded} {
				calls := 0
				integerOwner := false
				in, err := Instantiate(compiled, InstantiateOptions{Imports: testImports("env.step", func(v int32) int32 {
					calls++
					var pcs [64]uintptr
					frames := runtime.CallersFrames(pcs[:runtime.Callers(0, pcs[:])])
					for {
						frame, more := frames.Next()
						integerOwner = integerOwner || strings.Contains(frame.Function, "inlineHostIntegerContextEnter")
						if !more {
							break
						}
					}
					return v + 1
				})})
				if err != nil {
					t.Fatal(err)
				}
				p := in.eng.PreparedScalarHost()
				want := compiled == c && tc.want && armIntegerNumericEnabled && integerNumericHostEnabled && numericContextDetached(in)
				if p != nil && p.IntegerGuestContext() != want {
					t.Fatal("incorrect bridge admission")
				}
				for i := 0; i < 4; i++ {
					if _, err := in.Invoke("run", 1); err != nil {
						t.Fatal(err)
					}
				}
				calls, integerOwner = 0, false
				got, err := in.Invoke("run", 1)
				if err != nil || len(got) != 1 || got[0] != 2 || calls != 1 {
					t.Fatalf("invoke = %v, %v, callbacks %d", got, err, calls)
				}
				if integerOwner != want {
					t.Fatalf("integer Go owner reached = %t, want %t", integerOwner, want)
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
