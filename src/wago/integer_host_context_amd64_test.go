//go:build (linux || darwin) && amd64 && !tinygo

package wago

import (
	"bytes"
	"fmt"
	"math"
	"runtime"
	"testing"
)

func TestIntegerHostContextAdmission(t *testing.T) {
	// Artifact round-trips require explicit bounds checks, including guard-page builds.
	cfg := goHostSegmentConfig().WithBoundsChecks(BoundsChecksExplicit)
	for _, tc := range []struct {
		name, body, locals string
		want               bool
	}{
		{"integer", "local.get 0 call $step", "", true},
		{"integer-width", "local.get 0 call $step i64.extend_i32_u i32.wrap_i64", "(local i64)", true},
		{"unused-float-local", "local.get 0 call $step", "(local f64)", false},
		{"float-alu", "f32.const 1 f32.const 2 f32.add drop local.get 0 call $step", "", false},
		{"float-to-int", "f64.const 1 i32.trunc_f64_s call $step", "", false},
		{"reinterpret", "f32.const 1 i32.reinterpret_f32 call $step", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			module := fmt.Sprintf(`(module (import "env" "step" (func $step (param i32)(result i32))) (func (export "run") (param i32)(result i32) %s %s))`, tc.locals, tc.body)
			c, err := Compile(cfg, watToWasm(t, module))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if c.integerHostContextAllowed() != tc.want {
				t.Fatalf("integer proof=%t want %t", c.integerHostContextAllowed(), tc.want)
			}
			var artifact bytes.Buffer
			if _, err := c.WriteTo(&artifact); err != nil {
				t.Fatal(err)
			}
			decoded := new(Compiled)
			if _, err := decoded.ReadFrom(bytes.NewReader(artifact.Bytes())); err != nil {
				t.Fatal(err)
			}
			defer decoded.Close()
			if decoded.integerHostContextAllowed() {
				t.Fatal("artifact retained integer capability")
			}
			for _, code := range []*Compiled{c, decoded} {
				imports := NewImports()
				imports.HostFunc("env", "step", func(v int32) int32 { return v + 1 }).Params(ValI32).Results(ValI32)
				in, err := Instantiate(code, InstantiateOptions{Imports: imports})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				p := in.eng.PreparedScalarHost()
				if p != nil && p.IntegerGuestContext() != (integerNumericHostEnabled && numericContextDetached(in) && code == c && tc.want) {
					t.Fatal("incorrect bridge admission")
				}
				if _, err := in.Invoke("run", 1); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestIntegerHostContextPreservesCallbackFPState(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling uses ordinary context")
	}
	c, err := Compile(goHostSegmentConfig(), hostYieldLoopModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	floating, err := Compile(goHostSegmentConfig(), watToWasm(t, `(module
  (func (export "add") (param f32) (result f32) local.get 0 f32.const 1 f32.add))`))
	if err != nil {
		t.Fatal(err)
	}
	defer floating.Close()
	other, err := Instantiate(floating)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	for _, kind := range []string{"typed", "HostCall", "Caller"} {
		for _, api := range []string{"instance", "prepared", "session"} {
			t.Run(kind+"/"+api, func(t *testing.T) {
				runtime.LockOSThread()
				defer runtime.UnlockOSThread()
				original := readTestMXCSR()
				defer writeTestMXCSR(original)
				const initial = uint32(0x1f81)
				const changed = uint32(0x5f84) // round upward, preserve latest host status
				const precisionStatus = uint32(0x20)
				calls := 0
				step := func(v int32) int32 {
					want := initial
					if calls != 0 {
						want = changed
					}
					calls++
					if got := readTestMXCSR(); got&^precisionStatus != want&^precisionStatus {
						panic(fmt.Sprintf("host FP state %#x want %#x", got, want))
					}
					writeTestMXCSR(changed)
					// A nested floating guest still uses Wasm's nearest-even environment.
					got, err := other.Invoke("add", F32(16777216))
					if err != nil || len(got) != 1 || got[0] != uint64(math.Float32bits(16777216)) {
						panic(fmt.Sprintf("nested floating guest %v %v", got, err))
					}
					if got := readTestMXCSR(); got&^precisionStatus != changed&^precisionStatus {
						panic("nested guest changed host FP state")
					}
					if inlineWagoGrow(16) != 136 {
						panic("stack growth")
					}
					runtime.GC()
					return v + 1
				}
				var fn any = step
				if kind == "HostCall" {
					fn = func(call HostCall) { call.SetI32(0, step(call.I32(0))) }
				}
				if kind == "Caller" {
					fn = func(caller Caller, call HostCall) {
						if !caller.valid() {
							panic("expired Caller")
						}
						call.SetI32(0, step(call.I32(0)))
					}
				}
				imports := NewImports()
				imports.HostFunc("env", "step", fn).Params(ValI32).Results(ValI32)
				in, err := Instantiate(c, InstantiateOptions{Imports: imports})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				p := in.eng.PreparedScalarHost()
				if p == nil {
					t.Skip("prepared bridge unavailable")
				}
				if p.IntegerGuestContext() != (integerNumericHostEnabled && numericContextDetached(in)) {
					t.Fatal("integer bridge not selected")
				}
				invoke := func() ([]uint64, error) { return in.Invoke("run", 3, 0) }
				resolved, err := in.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				if api == "prepared" {
					invoke = func() ([]uint64, error) { return resolved.Invoke(3, 0) }
				}
				if api == "session" {
					s, err := resolved.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					defer s.Close()
					invoke = func() ([]uint64, error) { return s.Invoke2(3, 0) }
				}
				in.ensurePluginState()
				if _, err := in.fillInvokeCache("run"); err != nil {
					t.Fatal(err)
				}
				writeTestMXCSR(initial)
				got, err := invoke()
				if err != nil || len(got) != 1 || got[0] != 3 || calls != 3 {
					t.Fatalf("invoke %v %v calls=%d", got, err, calls)
				}
				if got := readTestMXCSR(); got&^precisionStatus != changed&^precisionStatus {
					t.Fatalf("latest host FP state %#x", got)
				}
			})
		}
	}
}
