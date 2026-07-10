//go:build arm64 && !tinygo

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
	rt := NewRuntime()
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
