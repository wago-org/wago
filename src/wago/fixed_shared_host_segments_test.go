//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestFixedSharedHostSegmentsAdmission(t *testing.T) {
	data := fixedSharedHostLoopModule(false)
	base, err := wasm.DecodeModule(data)
	if err != nil {
		t.Fatal(err)
	}
	if !boundedFixedSharedHostSegments(base) {
		t.Fatal("fixed shared yielding wrapper rejected")
	}
	for _, kind := range []string{"growable", "unshared", "memory64", "no-max", "multiple-memory", "table", "global", "imported-memory", "recursive", "unbounded", "conditional-callee", "pure-callee"} {
		t.Run(kind, func(t *testing.T) {
			m, err := wasm.DecodeModule(data)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "growable":
				m.Memories[0].Limits.Max++
			case "unshared":
				m.Memories[0].Shared = false
			case "memory64":
				m.Memories[0].Limits.Addr64 = true
			case "no-max":
				m.Memories[0].Limits.HasMax = false
			case "multiple-memory":
				m.Memories = append(m.Memories, m.Memories[0])
			case "table":
				m.Tables = append(m.Tables, wasm.Table{})
			case "global":
				m.Globals = append(m.Globals, wasm.Global{})
			case "imported-memory":
				m.Imports = append(m.Imports, m.Imports[0])
			case "recursive":
				m.Code[0].BodyBytes = []byte{0x20, 0, 0x10, 1, 0x0b}
			case "unbounded":
				m.Code[0].BodyBytes = []byte{0x03, 0x40, 0x0c, 0, 0x0b, 0x0b}
			case "conditional-callee":
				m.Code[0].BodyBytes = []byte{0x20, 0, 0x04, 0x40, 0x20, 0, 0x10, 0, 0x1a, 0x0b, 0x20, 0, 0x0b}
			case "pure-callee":
				m.Code[0].BodyBytes = []byte{0x20, 0, 0x0b}
			}
			if boundedFixedSharedHostSegments(m) {
				t.Fatal("unsafe scheduler certificate")
			}
		})
	}
	for _, enabled := range []bool{false, true} {
		cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit).WithOptimization("prepared-bounded-entry", enabled)
		c, err := Compile(cfg, data)
		if err != nil {
			t.Fatal(err)
		}
		if c.fixedSharedHostSegments != enabled || c.boundedHostSegments() {
			t.Fatal("certificate conflated with resource-free proof")
		}
		decoded := publicArtifactRoundTrip(t, c)
		if decoded.fixedSharedHostSegments {
			t.Fatal("artifact retained compile-only certificate")
		}
		decoded.Close()
		c.Close()
	}
}

func TestFixedSharedHostSegmentsEffectiveBounds(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksSignalsBased), fixedSharedHostLoopModule(false))
	if err == nil {
		c.Close()
		t.Fatal("shared memory unexpectedly accepted signal bounds")
	}
	if guardPageBuilt && !strings.Contains(err.Error(), "threads currently require explicit bounds checks") {
		t.Fatalf("signal bounds rejection = %v", err)
	}
}

func TestFixedSharedHostSegmentsCancellationAndGC(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	c, err := Compile(cfg, fixedSharedHostLoopModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	started := make(chan struct{})
	first := true
	in, err := Instantiate(c, testImports("env.step", func(v int32) int32 {
		if first {
			first = false
			close(started)
		}
		return v + 1
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if !in.boundedFixedSharedHostPortal() {
		t.Fatal("cancellation fixture rejected")
	}
	// Finish sweeping garbage from earlier tests before starting the busy loop.
	// On one P, runtime.GC yields between swept spans, letting the guest consume
	// a scheduling quantum each time even though collection is making progress.
	runtime.GC()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := in.InvokeContext(ctx, "run", 0x7fffffff); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("callback did not start")
	}
	collected := make(chan struct{})
	go func() { runtime.GC(); close(collected) }()
	select {
	case <-collected:
	case <-time.After(3 * time.Second):
		t.Fatal("GC made no progress")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("callback loop did not cancel")
	}
}

func TestFixedSharedHostSegmentsOwnedCallback(t *testing.T) {
	for _, session := range []bool{false, true} {
		for _, publish := range []bool{false, true} {
			t.Run(fmt.Sprintf("session=%t/publish=%t", session, publish), func(t *testing.T) {
				cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
				rt := NewRuntime(WithRuntimeConfig(cfg))
				defer rt.Close()
				mod, err := rt.Compile(fixedSharedHostLoopModule(false))
				if err != nil {
					t.Fatal(err)
				}
				defer mod.Close()
				var in *Instance
				calls := 0
				owner, err := rt.NewHostFuncRef(func(v int32) int32 {
					calls++
					if calls == 1 {
						runtime.GC()
						if publish {
							if _, e := in.ExportedFunc("run"); e != nil {
								panic(HostTrap{Err: e})
							}
						}
						if publish {
							if e := publishFixedSharedMemory(in); e != nil {
								panic(HostTrap{Err: e})
							}
						}
					}
					return v + 1
				}, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
				if err != nil {
					t.Fatal(err)
				}
				defer owner.Close()
				in, err = rt.Instantiate(context.Background(), mod, WithImports(NewImports().Function("env", "step", owner)))
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if !in.boundedFixedSharedHostPortal() {
					t.Fatal("fixed shared portal rejected")
				}
				flags := in.executionFlags.Load()
				in.executionFlags.Store(flags | executionFlagNativeScalarLeaf)
				if in.boundedFixedSharedHostPortal() {
					t.Fatal("native leaf mistaken for Go safe point")
				}
				in.executionFlags.Store(flags)

				fn, err := in.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				if fn.boundedNumericHost {
					t.Fatal("resourceful module admitted to resource-free bridge")
				}
				if e := owner.Close(); e == nil {
					t.Fatal("closed live host reference")
				}
				for i := 0; i < 2; i++ {
					var got []uint64
					if session {
						s, e := fn.OpenSession()
						if e != nil {
							t.Fatal(e)
						}
						got, err = s.Invoke1(128)
						s.Close()
					} else {
						got, err = fn.Invoke(128)
					}
					if err != nil || len(got) != 1 || got[0] != 8256 {
						t.Fatalf("callback loop=%v,%v", got, err)
					}
				}
				if calls != 256 {
					t.Fatalf("calls=%d", calls)
				}
			})
		}
	}
}

// Exercise public shared-memory publication while the native lease is parked.
func publishFixedSharedMemory(in *Instance) error {
	memory, err := in.ExportedMemory("memory")
	if err != nil {
		return err
	}
	if len(memory.UnsafeBytes()) != 65536 {
		return fmt.Errorf("shared memory unreadable")
	}
	return nil
}

func TestFixedSharedHostSegmentsTrapAfterCallback(t *testing.T) {
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	c, err := Compile(cfg, fixedSharedHostLoopModule(true))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	calls := 0
	hostErr := errors.New("callback aborted")
	in, err := Instantiate(c, testImports("env.step", func(v int32) int32 {
		calls++
		runtime.GC()
		if calls == 1 {
			panic(HostTrap{Err: hostErr})
		}
		if calls == 2 {
			panic(HostExit{Code: 17})
		}
		return v
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if !in.boundedFixedSharedHostPortal() {
		t.Fatal("trap fixture rejected")
	}
	if _, err := in.Invoke("run", 7); !errors.Is(err, hostErr) {
		t.Fatalf("host trap=%v", err)
	}
	if _, err := in.Invoke("run", 7); err == nil {
		t.Fatal("missing host exit")
	} else {
		var exit *ExitError
		if !errors.As(err, &exit) || exit.Code != 17 {
			t.Fatalf("host exit=%v", err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, err := in.Invoke("run", 7); err == nil {
			t.Fatal("missing bounds trap")
		}
	}
	if calls != 4 {
		t.Fatalf("calls=%d", calls)
	}
}
