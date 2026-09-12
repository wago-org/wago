//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	wruntime "github.com/wago-org/wago/src/core/runtime"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestHostCallbackGlobalAccess(t *testing.T) {
	if mode := os.Getenv("WAGO_TEST_CALLBACK_ACCESS"); mode != "" {
		testHostCallbackGlobalAccess(t, mode)
		return
	}
	for _, family := range []string{"typed", "expanded", "hostcall", "generic"} {
		for _, outcome := range []string{"return", "cancel", "host-error", "panic", "trap"} {
			mode := family + "/" + outcome
			t.Run(mode, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHostCallbackGlobalAccess$", "-test.count=1")
				cmd.Env = append(os.Environ(), "WAGO_TEST_CALLBACK_ACCESS="+mode)
				output, err := cmd.CombinedOutput()
				if ctx.Err() != nil {
					t.Fatalf("callback state access did not finish: %s", output)
				}
				if err != nil {
					t.Fatalf("callback subprocess: %v\n%s", err, output)
				}
			})
		}
	}
}

func testHostCallbackGlobalAccess(t *testing.T, mode string) {
	mode, outcome, _ := strings.Cut(mode, "/")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sentinel := errors.New("callback sentinel")
	body := []byte{0x20, 0, 0x10, 0}
	if outcome == "trap" {
		body = append(body, 0x00)
	}
	body = append(body, 0x0b)
	typ := wasm.I32
	if mode == "expanded" {
		typ = wasm.I64
	}
	source := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}))),
		wasmtest.Section(2, wasmtest.Vec(importEntry("env", "f", 0, 0))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x7f, 1, 0x41, 7, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("g", 0, 1), wasmtest.ExportEntry("value", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	c := MustCompile(source)
	defer c.Close()
	var in *Instance
	access := func(v int32) int32 {
		value, err := in.GlobalValue("value")
		if err != nil || value.I32() != 7 {
			panic(fmt.Errorf("callback global read = %v, %v", value, err))
		}
		if err := in.SetGlobalValue("value", ValueI32(40)); err != nil {
			panic(err)
		}
		// An unrelated goroutine must use the same lock, even during a callback.
		done := make(chan error, 1)
		started := make(chan struct{})
		unlock := in.lockInstanceNativeStateForHostAccess()
		go func() { close(started); done <- in.SetGlobalValue("value", ValueI32(41)) }()
		<-started
		select {
		case err := <-done:
			unlock()
			panic(fmt.Errorf("unrelated host access bypassed native mutex: %v", err))
		case <-time.After(10 * time.Millisecond):
		}
		unlock()
		if err := <-done; err != nil {
			panic(err)
		}

		switch outcome {
		case "cancel":
			cancel()
			// Keep the callback parked until the watcher has delivered cancellation.
			// Immediate return can legitimately win the race with the watcher.
			for atomic.LoadUint32((*uint32)(unsafe.Pointer(&in.trap[0]))) != uint32(wruntime.TrapInterrupted) {
				goruntime.Gosched()
			}
		case "host-error":
			panic(HostTrap{Err: sentinel})
		case "panic":
			panic(sentinel)
		}
		return v + 1
	}
	var host any
	switch mode {
	case "typed":
		host = func(v int32) int32 { return access(v) }
	case "expanded":
		host = func(v int64) int64 { return int64(access(int32(v))) }
	case "hostcall":
		host = HostCallFunc(func(c HostCall) { c.SetI32(0, access(c.I32(0))) })
	case "generic":
		host = HostFunc(func(_ HostModule, a, r []uint64) { r[0] = I32(access(AsI32(a[0]))) })
	}
	var err error
	in, err = Instantiate(c, InstantiateOptions{Imports: Imports{"env.f": host}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if !in.usesIndependentExecution() {
		t.Fatal("fixture requires independent execution")
	}
	var out []uint64
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		out, err = in.InvokeContext(ctx, "g", 41)
	}()
	switch outcome {
	case "return":
		if recovered != nil || err != nil || len(out) != 1 || out[0] != 42 {
			t.Fatalf("call = %v, %v, panic %v", out, err, recovered)
		}
	case "cancel":
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled callback = %v", err)
		}
	case "host-error":
		if !errors.Is(err, sentinel) {
			t.Fatalf("host error = %v", err)
		}
	case "panic":
		if recovered != sentinel {
			t.Fatalf("panic = %v", recovered)
		}
	case "trap":
		var trap *wruntime.TrapError
		if !errors.As(err, &trap) || trap.Code != wruntime.TrapUnreachable {
			t.Fatalf("guest trap = %v", err)
		}
	}
	if outcome != "panic" && recovered != nil {
		t.Fatalf("unexpected panic: %v", recovered)
	}

	value, err := in.GlobalValue("value")
	if err != nil || value.I32() != 41 {
		t.Fatalf("global after callback = %v, %v", value, err)
	}
}
