//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicPassiveDataLifetimeAndBounds(t *testing.T) {
	c, err := Compile(nil, passiveDataModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	if _, err := in.Invoke("init", 0xbadc0de000000003, 0xffff000000000000, 5); err != nil {
		t.Fatal(err)
	}
	mem := in.Memory().UnsafeBytes()
	if string(mem[3:8]) != "hello" || mem[2] != 0 || mem[8] != 0 {
		t.Fatal("passive payload or neighboring bytes", mem[:9])
	}
	if _, err := in.Invoke("drop"); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Invoke("init", 3, 0, 1); err == nil {
		t.Fatal("dropped payload remained visible")
	}
	if _, err := in.Invoke("init", 65536, 0, 0); err != nil {
		t.Fatal("empty end-of-memory operation", err)
	}
	if _, err := in.Invoke("init", 65537, 0, 0); err == nil {
		t.Fatal("empty out-of-bounds operation admitted")
	}
}

func rv64PublicSpinModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
		wasmtest.Section(5, []byte{1, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("spin", 0, 0), wasmtest.ExportEntry("value", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 0, 0x41, 1, 0x36, 2, 0, 0x03, 0x40, 0x0c, 0, 0x0b, 0x0b}),
			wasmtest.Code([]byte{0x41, 7, 0x0b}),
		)),
	)
}

func TestRV64PublicActiveCancellationAndClose(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	for _, action := range []string{"cancel", "close"} {
		t.Run(action, func(t *testing.T) {
			c, err := Compile(nil, rv64PublicSpinModule())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()

			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := in.InvokeContext(ctx, "spin"); done <- err }()
			// Observe an actual guest write before interrupting, so this cannot
			// pass solely through the pre-entry canceled-context check.
			flag := (*uint32)(unsafe.Pointer(&in.jm.CurrentBytes()[0]))
			deadline := time.Now().Add(5 * time.Second)
			for atomic.LoadUint32(flag) == 0 && time.Now().Before(deadline) {
				runtime.Gosched()
			}
			entered := atomic.LoadUint32(flag) != 0
			if action == "close" {
				err = in.Close()
			} else {
				cancel()
			}
			select {
			case callErr := <-done:
				if !entered || err != nil {
					t.Fatal("active entry/close", entered, err)
				}
				if action == "close" {
					requireInterruptedTrap(t, callErr)
				} else if !errors.Is(callErr, context.Canceled) {
					t.Fatal("active context cancellation", callErr)
				}
			case <-time.After(5 * time.Second):
				// A watchdog must not allow a failed poll test to unmap live code.
				stop := coreruntime.RequestInterruptAsync(in.trap)
				<-done
				stop()
				t.Fatal("active invocation did not stop")
			}
			if action != "close" {
				got, err := in.Invoke("value")
				if err != nil || !slices.Equal(got, []uint64{7}) {
					t.Fatal("cancellation leaked into the next call", got, err)
				}
			} else if _, err := in.Invoke("value"); err == nil {
				t.Fatal("closed instance admitted another call")
			}
		})
	}
}

func TestRV64PublicAdmissionRejectsBeforeMapping(t *testing.T) {
	base := multiValueControlCallModule()
	for _, shape := range []struct {
		name string
		data []byte
	}{
		{"empty-tag-section", wasmtest.Module(wasmtest.Section(13, []byte{0}))},
	} {
		t.Run(shape.name, func(t *testing.T) {
			compiled, err := Compile(nil, shape.data)
			if err == nil || compiled != nil {
				t.Fatal("unsupported shape published", compiled, err)
			}
			if strings.HasPrefix(shape.name, "empty-") && !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("empty unsupported section lost wire provenance", err)
			}
		})
	}
	// The artifact gate is independently required; a trusted producer can still
	// supply unsupported metadata without going through source compilation.
	c, err := Compile(nil, base)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	view := *c.executionView()
	view.HasStart = true
	if err := view.validate(); !errors.Is(err, railshot.ErrUnsupported) {
		t.Fatal("unsupported artifact metadata", err)
	}
	if c.codeCache.base != 0 || len(c.codeCache.mem) != 0 {
		t.Fatal("metadata admission published executable code")
	}
}

func TestRV64PublicActiveFloatCancellationAndColdReuse(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	// FP accumulator homes must remain valid around a cooperative backedge poll.
	body := []byte{1, 1, 0x7c, 3, 0x40, 0x20, 0, 0x44}
	body = append(body, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f)
	body = append(body, 0xa0, 0x21, 0, 0x41, 0, 0x41, 1, 0x36, 2, 0, 0x0c, 0, 0x0b, 0x00, 0x0b)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.F64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0))),
		wasmtest.Section(5, []byte{1, 0, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("spin", 0, 0), wasmtest.ExportEntry("value", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...), wasmtest.Code([]byte{0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0x0b}))),
	)
	c, err := Compile(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	in, err := Instantiate(c)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := in.InvokeContext(ctx, "spin"); done <- err }()
	flag := (*uint32)(unsafe.Pointer(&in.jm.CurrentBytes()[0]))
	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadUint32(flag) == 0 && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	entered := atomic.LoadUint32(flag) != 0
	cancel()
	select {
	case err := <-done:
		if !entered || !errors.Is(err, context.Canceled) {
			t.Fatal("FP loop active cancellation", entered, err)
		}
	case <-time.After(5 * time.Second):
		stop := coreruntime.RequestInterruptAsync(in.trap)
		<-done
		stop()
		t.Fatal("FP loop cancellation timed out")
	}
	got, err := in.Invoke("value")
	if err != nil || !slices.Equal(got, []uint64{0x3ff0000000000000}) {
		t.Fatal("FP cancellation cold reuse", got, err)
	}
}
