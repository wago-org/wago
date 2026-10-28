//go:build (linux || darwin) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

//go:noinline
func fixedIntegerGrow(n int) int {
	var frame [128]byte
	frame[n&127] = byte(n)
	if n == 0 {
		runtime.GC()
		return int(frame[0])
	}
	value := fixedIntegerGrow(n-1) + int(frame[n&127])
	runtime.KeepAlive(&frame)
	return value
}
func TestFixedSharedIntegerGoOwner(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run("native-leaf="+strconv.FormatBool(enabled), func(t *testing.T) { testFixedSharedIntegerGoOwner(t, enabled) })
	}
}
func testFixedSharedIntegerGoOwner(t *testing.T, nativeLeaf bool) {
	if codeProfileEnabled {
		t.Skip("profiling retains ordinary portal")
	}
	cfg := NewRuntimeConfig().WithOptimization("native-leaf-host", nativeLeaf).WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(fixedSharedHostLoopModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.fixedSharedHostInteger {
		t.Fatal("fixed shared integer register proof absent")
	}
	for _, bridgeEnabled := range []bool{false, true} {
		if bridgeEnabled {
			t.Setenv("WAGO_FIXED_SHARED_INTEGER_CONTEXT", "1")
		} else {
			t.Setenv("WAGO_FIXED_SHARED_INTEGER_CONTEXT", "0")
		}
		for artifact, code := range []*Compiled{c} {
			if code.fixedSharedHostInteger != (artifact == 0) {
				t.Fatal("artifact retained register proof")
			}
			for _, owned := range []bool{false, true} {
				calls := 0
				integerOwner := false
				callback := func(v int32) int32 {
					calls++
					if calls == 4 {
						_ = fixedIntegerGrow(128)
					}
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
				}
				rt := NewRuntime(WithRuntimeConfig(cfg))
				mod, err := rt.Module(code)
				if err != nil {
					t.Fatal(err)
				}
				imports := NewImports()
				var ref *HostFuncRef
				if owned {
					ref, err = rt.NewHostFuncRef(callback, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
					if err != nil {
						t.Fatal(err)
					}
					imports.Function("env", "step", ref)
				} else {
					imports.HostFunc("env", "step", callback)
				}
				in, err := rt.Instantiate(context.Background(), mod, WithImports(imports))
				if err != nil {
					t.Fatal(err)
				}
				if ref != nil {
					ref.Close()
				}
				fn, err := in.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 8; i++ {
					got, err := fn.Invoke(1)
					if err != nil || len(got) != 1 || got[0] != 1 {
						t.Fatalf("invoke=%v %v", got, err)
					}
				}
				if calls != 8 {
					t.Fatalf("callbacks=%d", calls)
				}
				minor, parseErr := strconv.Atoi(strings.Split(strings.TrimPrefix(runtime.Version(), "go1."), ".")[0])
				want := bridgeEnabled && artifact == 0 && integerNumericHostEnabled && (runtime.GOARCH == "amd64" || armIntegerNumericEnabled) && parseErr == nil && minor >= 22 && minor <= 27 && os.Getenv("WAGO_"+strings.ToUpper(runtime.GOARCH)+"_NO_INLINE_HOST") != "1"
				if integerOwner != want {
					t.Fatalf("integer owner=%v want=%v artifact=%d owned=%v", integerOwner, want, artifact, owned)
				}
				in.Close()
				mod.Close()
				rt.Close()
			}
		}
	}
}

func TestFixedSharedIntegerRegisterProofRejectsFP(t *testing.T) {
	for _, kind := range []string{"float", "float-memory", "float-local", "popcount", "simd", "growable", "unbounded"} {
		t.Run(kind, func(t *testing.T) {
			m, err := wasm.DecodeModule(fixedSharedHostLoopModule(false))
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "float":
				m.Code[0].BodyBytes = append([]byte{0x43, 0, 0, 0, 0, 0x1a}, m.Code[0].BodyBytes...)
			case "float-memory":
				m.Code[0].BodyBytes = append([]byte{0x41, 0, 0x2a, 2, 0, 0x1a}, m.Code[0].BodyBytes...)
				if integerHostModuleRegistersFor(m, true) {
					t.Fatal("FP memory access passed register proof")
				}
			case "float-local":
				m.Code[0].Locals.Runs = append(m.Code[0].Locals.Runs, wasm.LocalRun{Count: 1, Type: wasm.F64})
			case "popcount":
				m.Code[0].BodyBytes = append([]byte{0x41, 0, 0x69, 0x1a}, m.Code[0].BodyBytes...)
			case "simd":
				prefix := append([]byte{0xfd, 12}, make([]byte, 16)...)
				prefix = append(prefix, 0x1a)
				m.Code[0].BodyBytes = append(prefix, m.Code[0].BodyBytes...)
			case "growable":
				m.Memories[0].Limits.Max++
			case "unbounded":
				m.Code[0].BodyBytes = []byte{0x03, 0x40, 0x0c, 0, 0x0b, 0x41, 0, 0x0b}
			}
			if boundedFixedSharedHostSegments(m) && integerHostModuleRegistersFor(m, true) {
				t.Fatal("unsafe integer context admitted")
			}
		})
	}
}

// Exercise the actual context portal, including unwinding through the assembly
// bridge. A callback must run without the native mutex and reacquire it before
// panic cleanup restores the host trampoline.
func TestFixedSharedResumePayloadPanicAndVersion(t *testing.T) {
	if codeProfileEnabled {
		t.Skip("profiling retains ordinary portal")
	}
	t.Setenv("WAGO_FIXED_HOST_RESUME_PAYLOAD", "1")
	cfg := NewRuntimeConfig().WithFeature(CoreFeatureThreads, true).WithBoundsChecks(BoundsChecksExplicit)
	c, err := cfg.Compile(fixedSharedHostLoopModule(false))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var in *Instance
	calls, seen := 0, false
	imports := NewImports()
	imports.HostFunc("env", "step", func(v int32) int32 {
		calls++
		a := in.independentNativeExecutionMu()
		if !a.TryLock() {
			panic("native lease remained locked in callback")
		}
		a.Unlock()
		in.ensurePluginState().nativeContextVersion.Add(1)
		var pcs [64]uintptr
		frames := runtime.CallersFrames(pcs[:runtime.Callers(0, pcs[:])])
		for {
			f, more := frames.Next()
			seen = seen || strings.Contains(f.Function, "dispatchSingleTypedI32FixedResumeContext")
			if !more {
				break
			}
		}
		_ = fixedIntegerGrow(128)
		if calls == 1 {
			panic("resume payload sentinel")
		}
		return v + 1
	})
	in, err = Instantiate(c, InstantiateOptions{Imports: imports})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	var recovered any
	func() { defer func() { recovered = recover() }(); _, err = fn.Invoke(1) }()
	if recovered != "resume payload sentinel" {
		t.Fatalf("panic=%v err=%v", recovered, err)
	}
	for i := 0; i < 3; i++ {
		got, err := fn.Invoke(1)
		if err != nil || len(got) != 1 || got[0] != 1 {
			t.Fatalf("after panic=%v %v", got, err)
		}
	}
	if calls != 4 || !seen {
		t.Fatalf("calls=%d compact dispatcher=%v", calls, seen)
	}
}
