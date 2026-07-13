//go:build ((linux && amd64) || arm64) && !tinygo

package wago

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func TestCallContextInterruptsNativeLoop(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("spin", 0, 0),
			wasmtest.ExportEntry("value", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b}), // loop { br 0 }
			wasmtest.Code([]byte{0x41, 0x07, 0x0b}),
		)),
	)
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithInterruptible(true)))
	defer rt.Close()
	compiled, err := rt.Compile(mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), compiled)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := in.Call(ctx, "spin"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("spin error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancellation took %v, want bounded interruption", elapsed)
	}

	// The watcher must leave the shared trap cell clean for the next invocation.
	out, err := in.Call(context.Background(), "value")
	if err != nil || len(out) != 1 || out[0].I32() != 7 {
		t.Fatalf("post-cancel value = %v, %v; want 7", out, err)
	}
}

// TestInvokeContextInterruptsHostCallLoop guards the runaway-guest guard itself:
// a guest that calls a host import on every loop iteration must be interruptible
// by context, and must not be pre-empted by any fixed host-call re-entry cap. The
// loop here issues far more than the historical 1<<20 re-entry bound before its
// deadline; the cooperative trap-cell interrupt — not a "too many host calls"
// error — is what must break it.
func TestInvokeContextInterruptsHostCallLoop(t *testing.T) {
	i32 := []wasm.ValType{wasm.I32}
	// import env.tick : () -> i32
	imp := append(append(wasmtest.Name("env"), wasmtest.Name("tick")...), 0x00, 0x00)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, i32), // type 0: () -> i32 (the import)
			wasmtest.FuncType(nil, nil), // type 1: () -> ()  (spin)
		)),
		wasmtest.Section(2, wasmtest.Vec(imp)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(1))), // func 1 (spin) has type 1
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("spin", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			// spin(): loop { drop(call $tick); br 0 }
			wasmtest.Code([]byte{0x03, 0x40, 0x10, 0x00, 0x1a, 0x0c, 0x00, 0x0b, 0x0b}),
		)),
	)
	calls := 0
	c, err := CompileWithConfig(NewRuntimeConfig().WithInterruptible(true), mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := Instantiate(c, InstantiateOptions{Imports: Imports{"env.tick": HostFunc(func(_ HostModule, _, r []uint64) {
		calls++
		r[0] = I32(0)
	})}})
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := in.InvokeContext(ctx, "spin"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("spin error = %v, want context deadline (a re-entry-cap error here is the regression)", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("cancellation took %v, want bounded interruption", elapsed)
	}
	// Prove the loop sailed past the historical 1<<20 host-call re-entry cap:
	// interruption, not a synthetic bound, is what stopped it.
	if calls <= 1<<20 {
		t.Fatalf("host calls = %d, want > %d (loop must exceed the old cap to be a real regression guard)", calls, 1<<20)
	}
}

// TestInterruptibleToggleGatesPreemption proves the config toggle actually
// controls runtime interruption: the SAME bounded busy-loop module, compiled two
// ways, either honors an expiring deadline mid-run (interruptible) or runs to
// completion ignoring it (non-interruptible). It counts to 30M — long enough that
// the 2ms deadline always fires during the loop when safepoints are present.
func TestInterruptibleToggleGatesPreemption(t *testing.T) {
	// () -> i32 with one i32 local: loop { local.get 0; i32.const 1; i32.add;
	// local.tee 0; i32.const 30_000_000; i32.lt_u; br_if 0 }; local.get 0.
	body := []byte{
		0x03, 0x40, // loop (void)
		0x20, 0x00, // local.get 0
		0x41, 0x01, // i32.const 1
		0x6a,       // i32.add
		0x22, 0x00, // local.tee 0
		0x41, 0x80, 0x87, 0xa7, 0x0e, // i32.const 30000000 (uleb)
		0x49,       // i32.lt_u
		0x0d, 0x00, // br_if 0
		0x0b,       // end loop
		0x20, 0x00, // local.get 0
		0x0b, // end func
	}
	locals := []byte{0x01, 0x01, 0x7f} // one local group: 1 x i32
	fnBytes := append(append([]byte{}, locals...), body...)
	codeEntry := append(wasmtest.ULEB(uint32(len(fnBytes))), fnBytes...)
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("busy", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(codeEntry)),
	)

	run := func(interruptible bool) ([]uint64, error) {
		c, err := CompileWithConfig(NewRuntimeConfig().WithInterruptible(interruptible), mod)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatalf("instantiate: %v", err)
		}
		defer in.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Millisecond)
		defer cancel()

		return in.InvokeContext(ctx, "busy")
	}

	// Interruptible: the deadline fires mid-loop and preempts the guest.
	if _, err := run(true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("interruptible spin error = %v, want context deadline", err)
	}
	// Non-interruptible (default): no safepoints, so the guest cannot be preempted
	// — it runs the whole loop and returns the completed result, ignoring the
	// already-expired deadline.
	out, err := run(false)
	if err != nil {
		t.Fatalf("non-interruptible: unexpected error %v; guest must run to completion", err)
	}
	if len(out) != 1 || out[0] != 30_000_000 {
		t.Fatalf("non-interruptible busy() = %v, want 30000000 (ran to completion)", out)
	}
}

// TestInterruptibleConfigDefault documents the opt-in default at the config layer.
func TestInterruptibleConfigDefault(t *testing.T) {
	if NewRuntimeConfig().Interruptible() {
		t.Fatal("interruptibility must be off by default (opt-in)")
	}
	if !NewRuntimeConfig().WithInterruptible(true).Interruptible() {
		t.Fatal("WithInterruptible(true) must enable interruptibility")
	}
}

func TestInvokeContextInterruptsNativeLoop(t *testing.T) {
	mod := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, nil),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("spin", 0, 0),
			wasmtest.ExportEntry("value", 0, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b}), // loop { br 0 }
			wasmtest.Code([]byte{0x41, 0x07, 0x0b}),
		)),
	)
	rt := NewRuntime(WithRuntimeConfig(NewRuntimeConfig().WithInterruptible(true)))
	defer rt.Close()
	compiled, err := rt.Compile(mod)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	in, err := rt.Instantiate(context.Background(), compiled)
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	defer in.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := in.InvokeContext(ctx, "spin"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("spin error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancellation took %v, want bounded interruption", elapsed)
	}

	// The watcher must leave the shared trap cell clean for the next invocation.
	out, err := in.InvokeContext(context.Background(), "value")
	if err != nil || len(out) != 1 || out[0] != 7 {
		t.Fatalf("post-cancel value = %v, %v; want 7", out, err)
	}
}
