//go:build darwin && arm64 && !tinygo

package wago

import (
	"bytes"
	"context"
	"errors"
	goruntime "runtime"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/frontend"
	wruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func darwinInterruptSpinModule(importEntered bool) []byte {
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
	}
	localIndex := uint32(0)
	body := []byte{0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b}
	if importEntered {
		imp := append(append(wasmtest.Name("env"), wasmtest.Name("entered")...), 0x00, 0x00)
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(imp)))
		localIndex = 1
		body = append([]byte{0x10, 0x00}, body...)
	}
	sections = append(sections,
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("spin", 0, localIndex))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	return wasmtest.Module(sections...)
}

func darwinInterruptHostTransitionLoopModule() []byte {
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("entered")...), 0x00, 0x00)
	body := []byte{0x03, 0x40, 0x10, 0x00, 0x0c, 0x00, 0x0b, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("spin", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestDarwinARM64PublicCompileOmitsCooperativeInterruptPolls(t *testing.T) {
	raw := darwinInterruptSpinModule(false)
	mod, err := frontend.DecodeValidate(append([]byte(nil), raw...))
	if err != nil {
		t.Fatal(err)
	}
	withPolls, err := railshotCompileModuleWith(mod, railshotCompileOptions{Interruptible: true})
	if err != nil {
		t.Fatal(err)
	}
	withoutPolls, err := railshotCompileModuleWith(mod, railshotCompileOptions{Interruptible: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutPolls.Code) >= len(withPolls.Code) {
		t.Fatalf("poll-free code size = %d, cooperative = %d", len(withoutPolls.Code), len(withPolls.Code))
	}
	public, err := Compile(nil, append([]byte(nil), raw...))
	if err != nil {
		t.Fatal(err)
	}
	defer public.Close()
	if !bytes.Equal(public.code, withoutPolls.Code) {
		t.Fatal("public Darwin ARM64 compilation retained cooperative interrupt instrumentation")
	}
	if !wruntime.HostInterruptSupported() {
		t.Fatal("Darwin ARM64 did not report cold-path native interruption")
	}
}

func TestDarwinARM64DeadlineInterruptsDuringStopTheWorld(t *testing.T) {
	entered := make(chan struct{})
	in, err := Instantiate(MustCompile(darwinInterruptSpinModule(true)), InstantiateOptions{Imports: Imports{
		"env.entered": HostFunc(func(HostModule, []uint64, []uint64) { close(entered) }),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	callDone := make(chan error, 1)
	go func() {
		_, err := in.InvokeContext(ctx, "spin")
		callDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("spin function did not reach its host entry marker")
	}
	goruntime.GC()
	if err := <-callDone; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("spin error = %v, want context deadline", err)
	}
}

func TestDarwinARM64InterruptStressAcrossHostTransitionsAndGC(t *testing.T) {
	entered := make(chan struct{}, 1)
	in, err := Instantiate(MustCompile(darwinInterruptHostTransitionLoopModule()), InstantiateOptions{Imports: Imports{
		"env.entered": HostFunc(func(HostModule, []uint64, []uint64) {
			select {
			case entered <- struct{}{}:
			default:
			}
			goruntime.Gosched()
		}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	for iteration := 0; iteration < 32; iteration++ {
		select {
		case <-entered:
		default:
		}
		ctx, cancel := context.WithCancel(context.Background())
		callDone := make(chan error, 1)
		go func() {
			_, err := in.InvokeContext(ctx, "spin")
			callDone <- err
		}()
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			cancel()
			t.Fatalf("iteration %d did not enter host callback", iteration)
		}
		gcDone := make(chan struct{})
		go func() {
			goruntime.GC()
			close(gcDone)
		}()
		cancel()
		select {
		case err := <-callDone:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("iteration %d: spin error = %v, want context cancellation", iteration, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("iteration %d: cancellation did not return", iteration)
		}
		select {
		case <-gcDone:
		case <-time.After(2 * time.Second):
			t.Fatalf("iteration %d: concurrent GC did not finish", iteration)
		}
	}
}
