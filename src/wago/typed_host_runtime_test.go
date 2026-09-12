//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func typedHostGrowStack(depth int, value int32) int32 {
	var frame [256]byte
	frame[0] = byte(depth)
	if depth != 0 {
		value = typedHostGrowStack(depth-1, value)
	}
	runtime.KeepAlive(&frame)
	return value
}

func TestTypedI32HostCallbackMayGrowStackAndCollect(t *testing.T) {
	sig := wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})
	body := []byte{0x00, 0x20, 0x00, 0x10, 0x00, 0x0b}
	compiled := MustCompile(returningImportModule(sig, body))
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.f": I32ToI32HostFunc(func(value int32) int32 {
			value = typedHostGrowStack(128, value)
			runtime.GC()
			return value + 1
		}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if got, err := in.Invoke("g", I32(41)); err != nil || len(got) != 1 || AsI32(got[0]) != 42 {
		t.Fatalf("typed callback after stack growth and GC = %v, %v; want 42", got, err)
	}
}

func TestTypedI32HostCallbackRestoresAfterOtherInstanceRuns(t *testing.T) {
	otherCompiled := MustCompile(benchAddOneModule())
	defer otherCompiled.Close()
	other, err := Instantiate(otherCompiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	otherFn, err := other.PrepareI32ToI32("f")
	if err != nil {
		t.Fatal(err)
	}

	compiled := MustCompile(benchReturningImportModule())
	defer compiled.Close()
	in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.f": I32ToI32HostFunc(func(value int32) int32 {
			got, callErr := otherFn.Call(value)
			if callErr != nil {
				panic(callErr)
			}
			return got
		}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareI32ToI32("g")
	if err != nil {
		t.Fatal(err)
	}
	for i := int32(0); i < 100; i++ {
		got, err := fn.Call(i)
		if err != nil || got != i+1 {
			t.Fatalf("call %d after other instance = %d, %v; want %d", i, got, err, i+1)
		}
	}
}

func TestTypedI32HostCallbackRestoresAfterIndependentExecutionRevocation(t *testing.T) {
	compiled := MustCompile(benchReturningImportModule())
	defer compiled.Close()
	var in *Instance
	revoked := false
	var err error
	in, err = Instantiate(compiled, InstantiateOptions{Imports: Imports{
		"env.f": I32ToI32HostFunc(func(value int32) int32 {
			if !revoked {
				revoked = true
				in.markNativeControlShared()
			}
			return value + 1
		}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.PrepareI32ToI32("g")
	if err != nil {
		t.Fatal(err)
	}
	for i := int32(0); i < 100; i++ {
		got, err := fn.Call(i)
		if err != nil || got != i+1 {
			t.Fatalf("call %d after execution-mode revocation = %d, %v; want %d", i, got, err, i+1)
		}
	}
	if in.usesIndependentExecution() {
		t.Fatal("callback-time native-control sharing did not revoke independent execution")
	}
}

func TestTypedI32HostCallbackMayBlockWhileOtherInstanceRuns(t *testing.T) {
	for _, independent := range []bool{true, false} {
		t.Run(map[bool]string{true: "independent", false: "shared"}[independent], func(t *testing.T) {
			cfg := NewRuntimeConfig().WithIndependentInstanceExecution(independent)
			otherCompiled, err := cfg.Compile(benchAddOneModule())
			if err != nil {
				t.Fatal(err)
			}
			defer otherCompiled.Close()
			other, err := Instantiate(otherCompiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			otherFn, err := other.PrepareI32ToI32("f")
			if err != nil {
				t.Fatal(err)
			}

			entered := make(chan struct{})
			release := make(chan struct{})
			compiled, err := cfg.Compile(benchReturningImportModule())
			if err != nil {
				t.Fatal(err)
			}
			defer compiled.Close()
			in, err := Instantiate(compiled, InstantiateOptions{Imports: Imports{
				"env.f": I32ToI32HostFunc(func(value int32) int32 {
					close(entered)
					<-release
					return value + 1
				}),
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			fn, err := in.PrepareI32ToI32("g")
			if err != nil {
				t.Fatal(err)
			}

			rootDone := make(chan error, 1)
			go func() {
				got, callErr := fn.Call(41)
				if callErr == nil && got != 42 {
					callErr = fmt.Errorf("root result = %d, want 42", got)
				}
				rootDone <- callErr
			}()
			<-entered
			otherDone := make(chan error, 1)
			go func() {
				got, callErr := otherFn.Call(1)
				if callErr == nil && got != 2 {
					callErr = fmt.Errorf("other result = %d, want 2", got)
				}
				otherDone <- callErr
			}()
			select {
			case err := <-otherDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				close(release)
				t.Fatal("other instance blocked behind typed callback")
			}
			close(release)
			if err := <-rootDone; err != nil {
				t.Fatal(err)
			}
		})
	}
}
