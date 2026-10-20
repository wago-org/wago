//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	rt "github.com/wago-org/wago/src/core/runtime"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Keep each object in a global and dirty all of its fields before the next
// default allocation. The next object must still contain positive numeric zero.
func gcDefaultStructLoopBytes(final bool, fields int) []byte {
	types := []byte{0x7f, 0x7e, 0x7d, 0x7c}
	strct := []byte{0x5f, byte(fields)}
	for i := range fields {
		strct = append(strct, types[i%len(types)], 1)
	}
	if !final {
		strct = append([]byte{0x50, 0}, strct...)
	}
	body := []byte{1, 1, 0x7f, 0x02, 0x40, 0x03, 0x40, 0x20, 0, 0x45, 0x0d, 1,
		0xfb, 1, 0, 0x24, 0}
	for i := range fields {
		body = append(body, 0x20, 1, 0x23, 0, 0xfb, 2, 0, byte(i))
		switch types[i%len(types)] {
		case 0x7e:
			body = append(body, 0x50, 0x45) // i64.eqz; i32.eqz checks all bits
		case 0x7d:
			body = append(body, 0xbc) // i32.reinterpret_f32
		case 0x7c:
			body = append(body, 0xbd, 0x50, 0x45) // reinterpret then check all bits
		}
		body = append(body, 0x72, 0x21, 1) // accumulate every observed bit
	}
	for i := range fields {
		body = append(body, 0x23, 0)
		switch types[i%len(types)] {
		case 0x7f:
			body = append(body, 0x41, 42)
		case 0x7e:
			body = append(body, 0x42, 42)
		case 0x7d:
			body = append(body, 0x43, 0, 0, 0x80, 0x3f)
		case 0x7c:
			body = append(body, 0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f)
		}
		body = append(body, 0xfb, 5, 0, byte(i))
	}
	body = append(body, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x0c, 0, 0x0b, 0x0b, 0x20, 1, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(strct,
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{3})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("pair", 0, 1), wasmtest.ExportEntry("clear", 0, 2))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...),
			wasmtest.Code([]byte{0xfb, 1, 0, 0xfb, 1, 0, 0xd3, 0x0b}),
			wasmtest.Code([]byte{0xd0, 0, 0x24, 0, 0x0b}))),
	)
}

func defaultStructReservationCursor(in *Instance) uint32 {
	state := in.gc.NativeView().StructAllocState
	if state == 0 {
		return 0
	}
	bytes := unsafe.Slice((*byte)(offHeapPtr(state)), gc.NativeStructAllocStateSize)
	return binary.LittleEndian.Uint32(bytes[gc.NativeStructAllocCursorOffset:])
}

func TestGCDefaultStructNativeReservation(t *testing.T) {
	for _, fields := range []int{0, 4} {
		compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDefaultStructLoopBytes(true, fields))
		if err != nil {
			t.Fatal(err)
		}
		defer compiled.Close()
		in, err := Instantiate(compiled)
		if err != nil {
			t.Fatal(err)
		}
		defer in.Close()
		if out, err := in.Invoke("run", 12); err != nil || len(out) != 1 || out[0] != 0 {
			t.Fatalf("fields%d: %v, %v; want zero", fields, out, err)
		}
		if cursor := defaultStructReservationCursor(in); cursor == 0 {
			t.Fatalf("fields%d: default constructors did not consume a native reservation", fields)
		}
		if count := in.gc.Stats().Allocations; count != 12 {
			t.Fatalf("allocations=%d, want12", count)
		}
	}
}

func TestGCDefaultStructFallbackAndCollection(t *testing.T) {
	for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
		for _, final := range []bool{false, true} {
			for _, fields := range []int{0, 4, 5} {
				compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDefaultStructLoopBytes(final, fields))
				if err != nil {
					t.Fatal(err)
				}
				in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: profile, StressNurseryBytes: 256, CollectEveryAlloc: true, TinyCollectEveryAlloc: true, VerifyAfterCollect: true}})
				if err != nil {
					t.Fatal(err)
				}
				if out, err := in.Invoke("run", 256); err != nil || len(out) != 1 || out[0] != 0 {
					t.Fatalf("profile%v final%t fields%d: %v, %v", profile, final, fields, out, err)
				}
				if out, err := in.Invoke("pair"); err != nil || len(out) != 1 || out[0] != 0 {
					t.Fatalf("distinct simultaneously rooted defaults: %v, %v", out, err)
				}
				if count := in.gc.Stats().Allocations; count != 258 {
					t.Fatalf("allocations=%d, want258", count)
				}
				if _, err := in.Invoke("clear"); err != nil {
					t.Fatal(err)
				}
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
				if live := in.gc.Stats().LiveObjects; live != 0 {
					t.Fatalf("retained%d default objects", live)
				}
				in.Close()
				compiled.Close()
			}
		}
	}
}

func TestGCDefaultStructInterruptAndRecovery(t *testing.T) {
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDefaultStructLoopBytes(true, 4))
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	in, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.Invoke("run", 12); err != nil {
		t.Fatal(err)
	}
	before := in.gc.Stats()
	rt.RequestInterrupt(in.trap)
	_, err = in.Invoke("run", 7)
	requireInterruptedTrap(t, err)
	if after := in.gc.Stats(); after != before {
		t.Fatalf("interrupted entry changed allocation stats: %+v -> %+v", before, after)
	}
	if out, err := in.Invoke("run", 7); err != nil || len(out) != 1 || out[0] != 0 {
		t.Fatalf("recovery: %v, %v", out, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = in.InvokeContext(ctx, "run", 0x7fffffff)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("long allocation loop cancellation: %v", err)
	}
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
	allocations := in.gc.Stats().Allocations
	if out, err := in.Invoke("run", 64); err != nil || len(out) != 1 || out[0] != 0 {
		t.Fatalf("post-cancellation recovery: %v, %v", out, err)
	}
	if got := in.gc.Stats().Allocations - allocations; got != 64 {
		t.Fatalf("recovery allocations=%d, want64", got)
	}
	if _, err := in.Invoke("clear"); err != nil {
		t.Fatal(err)
	}
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
	if live := in.gc.Stats().LiveObjects; live != 0 {
		t.Fatalf("cancellation retained%d objects", live)
	}
}

func BenchmarkGCDefaultStructInvoke(b *testing.B) {
	for _, mode := range []string{"fast", "fallback", "mixed"} {
		b.Run(mode, func(b *testing.B) {
			compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcDefaultStructLoopBytes(true, 4))
			if err != nil {
				b.Fatal(err)
			}
			defer compiled.Close()
			cfg := GCConfig{Profile: GCProfileThroughput}
			if mode == "fallback" {
				cfg.Profile = GCProfileTiny
			} else if mode == "mixed" {
				cfg.StressNurseryBytes = 256
			}
			in, err := Instantiate(compiled, InstantiateOptions{GC: cfg})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			if out, err := in.Invoke("run", 256); err != nil || len(out) != 1 || out[0] != 0 {
				b.Fatalf("result %v, %v", out, err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := in.Invoke("run", 256); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(256, "guest-ops/op")
			b.ReportMetric(float64(compiled.CodeSize()), "code-B")
		})
	}
}

func BenchmarkGCDefaultStructCompile(b *testing.B) {
	module := gcDefaultStructLoopBytes(true, 4)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	var size int
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		compiled, err := Compile(cfg, module)
		if err != nil {
			b.Fatal(err)
		}
		size = compiled.CodeSize()
		compiled.Close()
	}
	b.ReportMetric(float64(size), "code-B")
}
