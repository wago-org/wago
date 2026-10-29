//go:build (linux || darwin) && amd64 && !tinygo

package wago

import (
	"fmt"
	"math"
	"runtime"
	"testing"
)

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
