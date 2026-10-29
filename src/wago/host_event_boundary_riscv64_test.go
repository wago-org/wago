//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	wruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"testing"
)

type hostEventObservation struct{ value, phase int32 }

func checkHostEventObservations(got, want []hostEventObservation) error {
	if len(got) != len(want) {
		return fmt.Errorf("event count: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].value != want[i].value {
			return fmt.Errorf("event order at %d", i)
		}
		if got[i].phase != want[i].phase {
			return fmt.Errorf("delivery phase at %d: got %d want %d", i, got[i].phase, want[i].phase)
		}
	}
	return nil
}

func hostEventBoundaryModule() []byte {
	event := append(wasmtest.Name("env"), wasmtest.Name("event")...)
	event = append(event, 0, 0)
	global := append(wasmtest.Name("env"), wasmtest.Name("phase")...)
	global = append(global, 3, 0x7f, 1)
	// (func (param $trap i32) (result i32) (local $seen i32)
	//   global.set phase(1); event(11); local.set seen(global.get phase);
	//   global.set phase(2); event(22); if trap { unreachable };
	//   global.set phase(3); return seen)
	body := []byte{1, 1, 0x7f,
		0x41, 1, 0x24, 0, 0x41, 11, 0x10, 0, 0x23, 0, 0x21, 1,
		0x41, 2, 0x24, 0, 0x41, 22, 0x10, 0,
		0x20, 0, 0x04, 0x40, 0x00, 0x0b,
		0x41, 3, 0x24, 0, 0x20, 1, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(event, global)),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestRV64HostEventNativeReturnBoundary(t *testing.T) {
	raw := hostEventBoundaryModule()
	for _, deferred := range []bool{false, true} {
		t.Run(fmt.Sprintf("deferred=%v", deferred), func(t *testing.T) {
			c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit).WithFunctionWorkers(1), append([]byte(nil), raw...))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			phase := NewGlobalI32(0, true)
			defer phase.Close()
			var got []hostEventObservation
			observe := func(value int32) { got = append(got, hostEventObservation{value, AsI32(phase.Get())}) }
			var callback any = I32HostEvent(observe)
			if !deferred {
				callback = i32HostFunc(func(value int32) {
					observe(value)
					if err := phase.Set(I32(100 + value)); err != nil {
						panic(HostTrap{Err: err})
					}
				})
			}
			in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.event", callback, "env.phase", phase)})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if in.syncMode == deferred {
				t.Fatal("wrong host delivery mode")
			}
			if in.c.codeCache == nil || !bytes.Equal(in.c.codeCache.mem[:len(in.c.code)], in.c.code) {
				t.Fatal("loaded event code differs from compiled image")
			}
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			for repeat := 0; repeat < 3; repeat++ {
				got = nil
				out, err := fn.Invoke(I32(0))
				if err != nil {
					t.Fatal(err)
				}
				want, wantResult := []hostEventObservation{{11, 1}, {22, 2}}, int32(111)
				if deferred {
					want, wantResult = []hostEventObservation{{11, 3}, {22, 3}}, 1
				}
				if err := checkHostEventObservations(got, want); err != nil {
					t.Fatal(err)
				}
				if len(out) != 1 || AsI32(out[0]) != wantResult || AsI32(phase.Get()) != 3 {
					t.Fatalf("result/state=%v/%d", out, AsI32(phase.Get()))
				}
			}
		})
	}
}

func TestRV64HostEventTrapAndReplayFailureBoundary(t *testing.T) {
	for _, guestTrap := range []bool{false, true} {
		t.Run(fmt.Sprintf("guest-trap=%v", guestTrap), func(t *testing.T) {
			raw := hostEventBoundaryModule()
			c, err := Compile(NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit), append([]byte(nil), raw...))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			phase := NewGlobalI32(0, true)
			defer phase.Close()
			var got []hostEventObservation
			sentinel := errors.New("stop event replay")
			fail := !guestTrap
			callback := I32HostEvent(func(value int32) {
				got = append(got, hostEventObservation{value, AsI32(phase.Get())})
				if fail {
					panic(HostTrap{Err: sentinel})
				}
			})
			in, err := Instantiate(c, InstantiateOptions{Imports: testImports("env.event", callback, "env.phase", phase)})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.WasmFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			trapArg := I32(0)
			if guestTrap {
				trapArg = I32(1)
			}
			_, err = fn.Invoke(trapArg)
			want := []hostEventObservation{{11, 3}}
			if guestTrap {
				want = nil
				var trap *wruntime.TrapError
				if !errors.As(err, &trap) || trap.Code != wruntime.TrapUnreachable {
					t.Fatalf("guest trap: %v", err)
				}
				if AsI32(phase.Get()) != 2 {
					t.Fatal("guest continued after trap")
				}
			} else if !errors.Is(err, sentinel) || AsI32(phase.Get()) != 3 {
				t.Fatalf("replay error/state: %v/%d", err, AsI32(phase.Get()))
			}
			if err := checkHostEventObservations(got, want); err != nil {
				t.Fatal(err)
			}
			fail = false
			got = nil
			out, err := fn.Invoke(I32(0))
			if err != nil || len(out) != 1 || AsI32(out[0]) != 1 {
				t.Fatalf("reuse after failure: %v/%v", out, err)
			}
			if err := checkHostEventObservations(got, []hostEventObservation{{11, 3}, {22, 3}}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
