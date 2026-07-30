//go:build linux && amd64 && !tinygo

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
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestPublicCompileOmitsCooperativeInterruptPolls(t *testing.T) {
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("spin", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b}),
		)),
	)
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
		t.Fatalf("poll-free code size = %d, cooperative = %d; want poll-free smaller", len(withoutPolls.Code), len(withPolls.Code))
	}
	t.Logf("spin module native code: poll-free=%d bytes cooperative=%d bytes", len(withoutPolls.Code), len(withPolls.Code))

	public, err := Compile(nil, append([]byte(nil), raw...))
	if err != nil {
		t.Fatal(err)
	}
	defer public.Close()
	if !bytes.Equal(public.Code, withoutPolls.Code) {
		t.Fatal("public Linux/amd64 compilation retained cooperative interrupt instrumentation")
	}
	if !wruntime.HostInterruptSupported() {
		t.Fatal("Linux/amd64 build did not report asynchronous host interruption")
	}
}

func TestKernelDeadlineInterruptsDuringStopTheWorld(t *testing.T) {
	entered := make(chan struct{})
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("entered")...), 0x00, 0x00)
	raw := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("spin", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x10, 0x00, 0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b}),
		)),
	)
	in, err := Instantiate(MustCompile(raw), InstantiateOptions{Imports: Imports{
		"env.entered": HostFunc(func(HostModule, []uint64, []uint64) { close(entered) }),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	callDone := make(chan error, 1)
	go func() {
		_, err := in.InvokeContext(ctx, "spin")
		callDone <- err
	}()
	<-entered
	goruntime.GC() // waits for native execution; the kernel timer must break it.
	if err := <-callDone; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("spin error = %v, want context deadline", err)
	}
}
