//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestGCExternLocalSharedCodeCallerOwner(t *testing.T) {
	for _, kind := range []string{"direct", "ref", "indirect"} {
		for _, differentImage := range []bool{false, true} {
			for _, closedProducer := range []bool{false, true} {
				name := fmt.Sprintf("%s/differentImage=%t/closedProducer=%t", kind, differentImage, closedProducer)
				t.Run(name, func(t *testing.T) { gcExternSharedCodeCallerOwner(t, kind, differentImage, closedProducer, nil) })
			}
		}
	}
}

func gcExternSharedCodeCallerOwner(t *testing.T, kind string, differentImage, closedProducer bool, inspect func(a, b *Instance)) {
	// The stack ABI exposes the caller frame below the callee wrapper. Both
	// instances share code, so a return PC alone cannot identify its owner.
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("reg-abi", false)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	settings := GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 512, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}
	noopData := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.AnyRef}))), wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("noop", 0, 0), wasmtest.ExportEntry("marker", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x1a, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0x0b}))),
	)
	nc, err := Compile(cfg, noopData)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	nm, err := rt.Module(nc)
	if err != nil {
		t.Fatal(err)
	}
	defer nm.Close()
	ni, err := rt.Instantiate(context.Background(), nm, WithGC(settings))
	if err != nil {
		t.Fatal(err)
	}
	defer ni.Close()
	noop, err := ni.ExportedFunc("noop")
	if err != nil {
		t.Fatal(err)
	}
	marker, err := ni.ExportedFunc("marker")
	if err != nil {
		t.Fatal(err)
	}
	run := []byte{1, 1, 0x6e, 0x20, 0, 0xfb, 26, 0x21, 1}
	switch kind {
	case "direct":
		run = append(run, 0x10, 0)
	case "ref":
		run = append(run, 0xd2, 0, 0x14, 2)
	case "indirect":
		run = append(run, 0x41, 0, 0x11, 2, 0)
	default:
		t.Fatalf("unknown call kind %q", kind)
	}
	run = append(run, 0x20, 1, 0xfb, 27, 0x0b)
	run = append(wasmtest.ULEB(uint32(len(run))), run...)
	tick := []byte{0xfb, 1, 0, 0x24, 0, 0xfb, 1, 0, 0x24, 0, 0xfb, 1, 0, 0x24, 0, 0x0b}
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.ExternRef}), wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.AnyRef}, []wasm.ValType{wasm.AnyRef}))),
		wasmtest.Section(2, wasmtest.Vec(instructionFuncImport("env", "tick", 2), instructionFuncImport("env", "marker", 3))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2}, []byte{3})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0, 1})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2), wasmtest.ExportEntry("tick", 0, 3), wasmtest.ExportEntry("identity", 0, 4))),
		wasmtest.Section(9, wasmtest.Vec([]byte{0, 0x41, 0, 0x0b, 1, 0})),
		wasmtest.Section(10, wasmtest.Vec(run, wasmtest.Code(tick), wasmtest.Code([]byte{0x20, 0, 0x0b}))),
	)
	c, err := Compile(cfg, data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	mod, err := rt.Module(c)
	if err != nil {
		t.Fatal(err)
	}
	defer mod.Close()
	a, err := rt.Instantiate(context.Background(), mod, WithImports(testImports("env.tick", noop, "env.marker", marker)), WithGC(settings))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	target, err := a.ExportedFunc("tick")
	if err != nil {
		t.Fatal(err)
	}
	callerModule := mod
	if differentImage {
		callerCode, compileErr := Compile(cfg, data)
		if compileErr != nil {
			t.Fatal(compileErr)
		}
		defer callerCode.Close()
		callerModule, err = rt.Module(callerCode)
		if err != nil {
			t.Fatal(err)
		}
		defer callerModule.Close()
	}
	b, err := rt.Instantiate(context.Background(), callerModule, WithImports(testImports("env.tick", target, "env.marker", marker)), WithGC(settings))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if a.gc != b.gc || (a.base != b.base) != differentImage {
		t.Fatal("fixture does not have the requested collector/code identity")
	}
	if closedProducer {
		a.Close()
	}
	if inspect != nil {
		inspect(a, b)
		return
	}
	foreign, err := b.NewExternRef("B-local-across-A-allocation")
	if err != nil {
		t.Fatal(err)
	}
	word := ValueExternRef(foreign).Bits()
	before := a.gc.Stats().FullCollections
	out, err := b.Invoke("run", word)
	if err != nil || len(out) != 1 || out[0] != word {
		t.Fatalf("valid B-local across A allocation: %x,%v; want%x", out, err, word)
	}
	if a.gc.Stats().FullCollections <= before {
		t.Fatal("callee did not collect")
	}
}

func TestGCExternFrameCallerContext(t *testing.T) {
	gcExternSharedCodeCallerOwner(t, "direct", false, false, func(a, b *Instance) {
		foreign, err := b.NewExternRef("caller-frame")
		if err != nil {
			t.Fatal(err)
		}
		word, err := b.existingGCRefTestTableState().Conversion.anyFromExtern(ValueExternRef(foreign).Bits())
		if err != nil {
			t.Fatal(err)
		}
		// Exercise both persisted native frame layouts on the current host.
		for _, layout := range []uint8{gcNativeFrameLayoutAMD64, gcNativeFrameLayoutARM64} {
			for _, tc := range []struct {
				name    string
				context uintptr
				root    uint64
				reject  bool
			}{
				{"caller", b.nativeContext, word, false},
				{"callee", a.nativeContext, word, true},
				{"unknown", b.nativeContext | 1, word, true},
				{"compact", b.nativeContext, uint64(gc.I31New(9)), false},
				{"forged-root", b.nativeContext, 1<<32 | 1, true},
			} {
				t.Run(fmt.Sprintf("layout=%d/%s", layout, tc.name), func(t *testing.T) {
					frame := make([]byte, 256)
					base := uintptr(unsafe.Pointer(&frame[0]))
					recordBytes, returnBias, contextBias := 8, 0, 8
					if layout == gcNativeFrameLayoutARM64 {
						recordBytes, returnBias, contextBias = shared.ARM64FrameRecordBytes, shared.ARM64SavedLROffset, 0
					}
					callerBase := 32 + recordBytes + 64
					binary.LittleEndian.PutUint64(frame[16:], uint64(gc.I31New(3)))
					binary.LittleEndian.PutUint64(frame[32+returnBias:], uint64(a.base+80))
					binary.LittleEndian.PutUint64(frame[32+recordBytes+contextBias:], uint64(tc.context))
					binary.LittleEndian.PutUint64(frame[callerBase+16:], tc.root)
					binary.LittleEndian.PutUint64(frame[callerBase+32+returnBias:], uint64(a.base+200))
					roots := gcNativeFrameRoots{owner: a, base: base, offsets: []uint32{16}, frameBytes: 32, frameLayout: layout, codeBase: a.base, codeBytes: uintptr(len(a.c.code)), adapterReturnOffsets: []uint32{200}, callsites: []compiledGCFrameCallsite{{returnOffset: 80, frameBytes: 32, stackAdjust: 64, offsets: []uint32{16}}}}
					seen := 0
					var failure any
					func() {
						defer func() { failure = recover() }()
						roots.rangeChain(func(slot gc.RootSlot) bool { seen++; return true }, nil, nil)
					}()
					if (failure != nil) != tc.reject {
						t.Fatalf("failure=%v, want reject=%t", failure, tc.reject)
					}
					if !tc.reject {
						want := 1
						if tc.name == "compact" {
							want = 2
						}
						if seen != want {
							t.Fatalf("visited %d roots, want %d", seen, want)
						}
					}
					runtime.KeepAlive(frame)
				})
			}
		}
	})
}
