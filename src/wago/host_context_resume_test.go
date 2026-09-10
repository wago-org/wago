//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"testing"
	"time"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestHostContextResumeMatchesForcedRestore(t *testing.T) {
	t.Run("legacy", func(t *testing.T) { testHostContextResumeMatchesForcedRestore(t, false) })
	t.Run("concrete", func(t *testing.T) { testHostContextResumeMatchesForcedRestore(t, true) })
}

func testHostContextResumeMatchesForcedRestore(t *testing.T, concrete bool) {
	c := MustCompile(watToWasm(t, `(module
 (import "env" "step" (func $step))
 (memory (export "memory") 1 3)
 (global $g (export "global") (mut i32) (i32.const 0))
 (func (export "grow") (result i32) i32.const 1 memory.grow)
 (func (export "inner") (result i32) i32.const 7)
 (func (export "set") i32.const 19 global.set $g)
 (func (export "run") (result i32 i32 i32)
  call $step memory.size i32.const 0 i32.load global.get $g))`))
	defer c.Close()
	for _, action := range []string{"noop", "read", "write", "guarded", "grow", "nested", "global", "export", "gc", "panic", "exit", "trap", "cancel"} {
		t.Run(action, func(t *testing.T) {
			var want []uint64
			var wantOutcome string
			for _, force := range []bool{false, true} {
				ctx, cancel := context.WithCancel(context.Background())
				var in *Instance
				var callbackVersion uint64
				var caller instanceHostModule
				in, err := Instantiate(c, Imports{"env.step": callerTestCallback(concrete, func(mod HostModule, _, _ []uint64) {
					caller, _ = resolveHostCaller(mod)
					callbackVersion = in.pluginState.Load().nativeContextVersion.Load()
					nested := func(name string) {
						if _, err := in.InvokeFromHost(ctx, mod, name); err != nil {
							panic(err)
						}
					}
					switch action {
					case "read":
						if len(mod.Memory()) != 65536 {
							panic("bad memory size")
						}
					case "write":
						mod.Memory()[0] = 23
					case "guarded":
						if err := caller.WithGuestStorage(func(GuestStorage) error { return nil }); err != nil {
							panic(err)
						}
					case "grow":
						nested("grow")
					case "nested":
						nested("inner")
					case "global":
						nested("set")
					case "export":
						if _, err := in.ExportedMemory("memory"); err != nil {
							panic(err)
						}
					case "gc":
						runtime.GC()
					case "panic":
						panic("callback panic")
					case "exit":
						panic(HostExit{Code: 3})
					case "trap":
						panic(HostTrap{Err: fmt.Errorf("callback trap")})
					case "cancel":
						cancel()
						// Cancellation is asynchronous. Keep native frames parked until
						// the watcher publishes the interrupt, then compare resume paths.
						deadline := time.Now().Add(time.Second)
						for coreruntime.PreparedIntTrapCode(in.trap) != coreruntime.TrapInterrupted {
							if time.Now().After(deadline) {
								t.Error("cancellation watcher did not publish interrupt")
								return
							}
							runtime.Gosched()
						}
					}
				})})
				if err != nil {
					t.Fatal(err)
				}
				if force {
					in.ensurePluginState().nativeContextVersion.Store(^uint64(0))
				}
				var got []uint64
				outcome := ""
				func() {
					defer func() {
						if p := recover(); p != nil {
							outcome = fmt.Sprintf("panic: %v", p)
						}
					}()
					got, err = in.InvokeContext(ctx, "run")
					outcome = fmt.Sprint(err)
				}()
				got = append([]uint64(nil), got...)
				switch action {
				case "panic", "exit", "trap", "cancel":
					if outcome == "<nil>" {
						t.Error("exceptional callback completed without error")
					}
				default:
					expected := []uint64{1, 0, 0}
					switch action {
					case "grow":
						expected[0] = 2
					case "write":
						expected[1] = 23
					case "global":
						expected[2] = 19
					}
					if outcome != "<nil>" || !reflect.DeepEqual(got, expected) {
						t.Errorf("result=%v/%s, want %v", got, outcome, expected)
					}
				}
				if caller.valid() || isNativeActive(in, caller.invocationID) {
					t.Error("callback state survived unwind")
				}
				if !force {
					want, wantOutcome = got, outcome
					unchanged := action == "noop" || action == "read" || action == "write" || action == "gc" || action == "panic" || action == "exit" || action == "trap" || action == "cancel"
					if in.canReuseParkedNativeContext(callbackVersion) != unchanged {
						t.Errorf("context reuse for %s does not match %v", action, unchanged)
					}
				} else if !reflect.DeepEqual(want, got) || outcome != wantOutcome {
					t.Errorf("safe=%v/%s; reuse=%v/%s", got, outcome, want, wantOutcome)
				}
				cancel()
				in.Close()
			}
		})
	}
}

func TestHostContextVersionSaturates(t *testing.T) {
	var in Instance
	v := &in.ensurePluginState().nativeContextVersion
	v.Store(^uint64(0) - 1)
	in.invalidateNativeContext()
	in.invalidateNativeContext()
	if v.Load() != ^uint64(0) || in.canReuseParkedNativeContext(^uint64(0)) {
		t.Fatal("exhausted version admitted reuse")
	}
}
