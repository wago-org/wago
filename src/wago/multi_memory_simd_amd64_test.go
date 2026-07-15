//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func indexedSIMDMemOp(sub uint32, align byte, offset uint32, lane ...byte) []byte {
	op := []byte{0xfd}
	op = append(op, wasmtest.ULEB(sub)...)
	op = append(op, align|0x40, 0x01)
	op = append(op, wasmtest.ULEB(offset)...)
	return append(op, lane...)
}

func indexedSIMDModule(params, results []wasm.ValType, body []byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, results))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec(
			[]byte{0x01, 0x01, 0x01},
			[]byte{0x01, 0x01, 0x01},
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 0),
			wasmtest.ExportEntry("m0", 2, 0),
			wasmtest.ExportEntry("m1", 2, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func invokeV128(t *testing.T, in *Instance, name string, args ...uint64) V128 {
	t.Helper()
	got, err := in.Invoke(name, args...)
	if err != nil {
		t.Fatalf("invoke %s: %v", name, err)
	}
	if len(got) != 2 {
		t.Fatalf("invoke %s result slots = %d, want 2", name, len(got))
	}
	return slotsToV128(got)
}

func v128RawSlots(v V128) (uint64, uint64) {
	return binary.LittleEndian.Uint64(v[:8]), binary.LittleEndian.Uint64(v[8:])
}

func stagedSIMDInstance(t *testing.T, module []byte) (*Compiled, *Instance, *Memory) {
	t.Helper()
	compiled := stagedMultiMemoryCompile(t, module)
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatalf("instantiate staged SIMD multi-memory module: %v", err)
	}
	t.Cleanup(func() { _ = in.Close() })
	m1, err := in.ExportedMemory("m1")
	if err != nil {
		t.Fatalf("export memory 1: %v", err)
	}
	return compiled, in, m1
}

func TestStagedMultiMemorySIMDLoadsAndStores(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}

	t.Run("v128 load store and isolation", func(t *testing.T) {
		store := indexedSIMDMemOp(11, 4, 5)
		load := indexedSIMDMemOp(0, 4, 5)
		body := append([]byte{0x20, 0x00, 0x20, 0x01}, store...)
		body = append(body, 0x20, 0x00)
		body = append(body, load...)
		body = append(body, 0x0b)
		_, in, m1 := stagedSIMDInstance(t, indexedSIMDModule([]wasm.ValType{wasm.I32, wasm.V128}, []wasm.ValType{wasm.V128}, body))
		want := V128{0xde, 0xad, 0xbe, 0xef, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
		lo, hi := v128RawSlots(want)
		if got := invokeV128(t, in, "run", I32(3), lo, hi); got != want {
			t.Fatalf("indexed v128 round trip = % x, want % x", got, want)
		}
		if got := V128(m1.Bytes()[8:24]); got != want {
			t.Fatalf("memory 1 bytes = % x, want % x", got, want)
		}
		m0, err := in.ExportedMemory("m0")
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range m0.Bytes()[8:24] {
			if b != 0 {
				t.Fatalf("memory 0 changed through indexed SIMD store: % x", m0.Bytes()[8:24])
			}
		}
		if _, err := in.Invoke("run", I32(65516), lo, hi); err == nil {
			t.Fatal("cross-boundary indexed v128.store unexpectedly succeeded")
		}
	})

	loads := []struct {
		name        string
		sub         uint32
		align       byte
		size        int
		input, want V128
	}{
		{name: "v128.load", sub: 0, align: 4, size: 16, input: V128{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}, want: V128{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}},
		{name: "v128.load8x8_s", sub: 1, align: 3, size: 8, input: V128{0, 1, 0x7f, 0x80, 0xff, 0xa5, 0x34, 0xfe}, want: V128{0, 0, 1, 0, 0x7f, 0, 0x80, 0xff, 0xff, 0xff, 0xa5, 0xff, 0x34, 0, 0xfe, 0xff}},
		{name: "v128.load8x8_u", sub: 2, align: 3, size: 8, input: V128{0, 1, 0x7f, 0x80, 0xff, 0xa5, 0x34, 0xfe}, want: V128{0, 0, 1, 0, 0x7f, 0, 0x80, 0, 0xff, 0, 0xa5, 0, 0x34, 0, 0xfe, 0}},
		{name: "v128.load16x4_s", sub: 3, align: 3, size: 8, input: V128{0, 0, 0xff, 0x7f, 0, 0x80, 0x34, 0xff}, want: V128{0, 0, 0, 0, 0xff, 0x7f, 0, 0, 0, 0x80, 0xff, 0xff, 0x34, 0xff, 0xff, 0xff}},
		{name: "v128.load16x4_u", sub: 4, align: 3, size: 8, input: V128{0, 0, 0xff, 0x7f, 0, 0x80, 0x34, 0xff}, want: V128{0, 0, 0, 0, 0xff, 0x7f, 0, 0, 0, 0x80, 0, 0, 0x34, 0xff, 0, 0}},
		{name: "v128.load32x2_s", sub: 5, align: 3, size: 8, input: V128{0xff, 0xff, 0xff, 0x7f, 0, 0, 0, 0x80}, want: V128{0xff, 0xff, 0xff, 0x7f, 0, 0, 0, 0, 0, 0, 0, 0x80, 0xff, 0xff, 0xff, 0xff}},
		{name: "v128.load32x2_u", sub: 6, align: 3, size: 8, input: V128{0xff, 0xff, 0xff, 0x7f, 0, 0, 0, 0x80}, want: V128{0xff, 0xff, 0xff, 0x7f, 0, 0, 0, 0, 0, 0, 0, 0x80, 0, 0, 0, 0}},
		{name: "v128.load8_splat", sub: 7, align: 0, size: 1, input: V128{0xa5}, want: V128{0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5, 0xa5}},
		{name: "v128.load16_splat", sub: 8, align: 1, size: 2, input: V128{0x34, 0x12}, want: V128{0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12}},
		{name: "v128.load32_splat", sub: 9, align: 2, size: 4, input: V128{0x78, 0x56, 0x34, 0x12}, want: V128{0x78, 0x56, 0x34, 0x12, 0x78, 0x56, 0x34, 0x12, 0x78, 0x56, 0x34, 0x12, 0x78, 0x56, 0x34, 0x12}},
		{name: "v128.load64_splat", sub: 10, align: 3, size: 8, input: V128{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}, want: V128{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}},
		{name: "v128.load32_zero", sub: 92, align: 2, size: 4, input: V128{0xef, 0xcd, 0xab, 0x89}, want: V128{0xef, 0xcd, 0xab, 0x89}},
		{name: "v128.load64_zero", sub: 93, align: 3, size: 8, input: V128{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}, want: V128{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}},
	}
	for _, tc := range loads {
		t.Run(tc.name, func(t *testing.T) {
			body := append([]byte{0x20, 0x00}, indexedSIMDMemOp(tc.sub, tc.align, 5)...)
			body = append(body, 0x0b)
			_, in, m1 := stagedSIMDInstance(t, indexedSIMDModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}, body))
			copy(m1.Bytes()[8:], tc.input[:tc.size])
			if got := invokeV128(t, in, "run", I32(3)); got != tc.want {
				t.Fatalf("%s = % x, want % x", tc.name, got, tc.want)
			}
			if _, err := in.Invoke("run", I32(int32(65536-5-int(tc.size)+1))); err == nil {
				t.Fatalf("%s cross-boundary access unexpectedly succeeded", tc.name)
			}
		})
	}

	t.Run("offset overflow traps", func(t *testing.T) {
		body := append([]byte{0x20, 0x00}, indexedSIMDMemOp(0, 4, ^uint32(0))...)
		body = append(body, 0x0b)
		_, in, _ := stagedSIMDInstance(t, indexedSIMDModule([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}, body))
		if _, err := in.Invoke("run", I32(1)); err == nil {
			t.Fatal("indexed SIMD offset overflow unexpectedly succeeded")
		}
	})
}

func TestStagedMultiMemorySIMDLanes(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	lanes := []struct {
		name              string
		loadSub, storeSub uint32
		align, size, lane byte
	}{
		{name: "8", loadSub: 84, storeSub: 88, align: 0, size: 1, lane: 13},
		{name: "16", loadSub: 85, storeSub: 89, align: 1, size: 2, lane: 6},
		{name: "32", loadSub: 86, storeSub: 90, align: 2, size: 4, lane: 2},
		{name: "64", loadSub: 87, storeSub: 91, align: 3, size: 8, lane: 1},
	}
	for _, tc := range lanes {
		t.Run(tc.name, func(t *testing.T) {
			loadBody := append([]byte{0x20, 0x00, 0x20, 0x01}, indexedSIMDMemOp(tc.loadSub, tc.align, 5, tc.lane)...)
			loadBody = append(loadBody, 0x0b)
			_, loadIn, loadMem := stagedSIMDInstance(t, indexedSIMDModule([]wasm.ValType{wasm.I32, wasm.V128}, []wasm.ValType{wasm.V128}, loadBody))
			for i := byte(0); i < tc.size; i++ {
				loadMem.Bytes()[8+int(i)] = 0xa0 + i
			}
			initial := V128{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
			want := initial
			copy(want[int(tc.lane)*int(tc.size):], loadMem.Bytes()[8:8+int(tc.size)])
			lo, hi := v128RawSlots(initial)
			if got := invokeV128(t, loadIn, "run", I32(3), lo, hi); got != want {
				t.Fatalf("load lane = % x, want % x", got, want)
			}
			if _, err := loadIn.Invoke("run", I32(int32(65536-5-int(tc.size)+1)), lo, hi); err == nil {
				t.Fatal("cross-boundary indexed SIMD lane load unexpectedly succeeded")
			}

			storeBody := append([]byte{0x20, 0x00, 0x20, 0x01}, indexedSIMDMemOp(tc.storeSub, tc.align, 5, tc.lane)...)
			storeBody = append(storeBody, 0x0b)
			_, storeIn, storeMem := stagedSIMDInstance(t, indexedSIMDModule([]wasm.ValType{wasm.I32, wasm.V128}, nil, storeBody))
			if _, err := storeIn.Invoke("run", I32(3), lo, hi); err != nil {
				t.Fatalf("store lane: %v", err)
			}
			laneStart := int(tc.lane) * int(tc.size)
			if got, wantBytes := storeMem.Bytes()[8:8+int(tc.size)], initial[laneStart:laneStart+int(tc.size)]; string(got) != string(wantBytes) {
				t.Fatalf("stored lane bytes = % x, want % x", got, wantBytes)
			}
			before := append([]byte(nil), storeMem.Bytes()[8:8+int(tc.size)]...)
			if _, err := storeIn.Invoke("run", I32(int32(65536-5-int(tc.size)+1)), lo, hi); err == nil {
				t.Fatal("cross-boundary indexed SIMD lane store unexpectedly succeeded")
			}
			if got := storeMem.Bytes()[8 : 8+int(tc.size)]; string(got) != string(before) {
				t.Fatalf("trapping lane store changed memory: % x", got)
			}
		})
	}
}

func TestStagedMultiMemorySIMDMemoryZeroCodeUnchanged(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	body := append([]byte{0x20, 0x00}, []byte{0xfd, 0x00, 0x04, 0x00}...)
	body = append(body, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.V128}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0x01, 0x01, 0x01})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	t.Setenv("WAGO_BOUNDS", "explicit")
	cfg := NewRuntimeConfig()
	baseFeatures := cfg.frontendFeatures()
	base, err := compileWithFrontendFeatures(cfg, module, baseFeatures)
	if err != nil {
		t.Fatalf("compile baseline: %v", err)
	}
	defer base.Close()
	stagedFeatures := baseFeatures
	stagedFeatures.MultiMemory = true
	staged, err := compileWithFrontendFeatures(cfg, module, stagedFeatures)
	if err != nil {
		t.Fatalf("compile staged: %v", err)
	}
	defer staged.Close()
	if string(base.Code) != string(staged.Code) {
		t.Fatal("enabling staged multi-memory changed ordinary SIMD memory-0 code bytes")
	}
}
