//go:build linux && (amd64 || arm64) && !tinygo

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/wago-org/wago/src/core/runtime/gc/native"
	goruntime "runtime"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// No imports or callbacks are needed to collect while these Wasm locals are live.
func gcExternLocalRootsModule(op byte) []byte {
	typ := wasm.ExternRef
	if op == 27 {
		typ = wasm.AnyRef
	}
	body := []byte{1, 1, 0x63, 0, 0x41, 0xc9, 0, 0xfb, 0, 0, 0x21, 1, 0x20, 0, 0xfb, op, 0xd1, 0x1a}
	for i := 0; i < 3; i++ {
		body = append(body, 0xfb, 1, 0, 0x24, 0)
	}
	body = append(body, 0x20, 1, 0xfb, 2, 0, 0, 0x0b)
	body = append(wasmtest.ULEB(uint32(len(body))), body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(body)),
	)
}

func TestGCExternConversionKeepsLocalRoots(t *testing.T) {
	for _, op := range []byte{26, 27} {
		for _, profile := range []struct {
			name string
			gc   GCConfig
		}{
			{"throughput", GCConfig{CollectEveryAlloc: true, StressNurseryBytes: 64, VerifyAfterCollect: true}},
			{"tiny", GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}},
		} {
			t.Run(fmt.Sprintf("op%d/%s", op, profile.name), func(t *testing.T) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcExternLocalRootsModule(op))
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c, InstantiateOptions{GC: profile.gc})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				for i := 0; i < 3; i++ {
					got, err := in.Invoke("run", 0)
					if err != nil || len(got) != 1 || got[0] != 73 {
						t.Fatalf("live local after collection: %v,%v; want73", got, err)
					}
				}
				admission := c.GCNativeRootAdmission()
				if !admission.Required || !admission.Exact || admission.Safepoints == 0 || admission.MaximumRoots == 0 {
					t.Fatalf("missing allocation roots: %+v", admission)
				}
			})
		}
	}
}

func TestGCExternConversionKeepsVectorAndLocalRoots(t *testing.T) {
	payload := []byte{1, 0x82, 3, 0xf4, 5, 0xa6, 7, 0xe8, 9, 0xba, 11, 0xdc, 13, 0xfe, 15, 0x90}
	for _, op := range []byte{26, 27} {
		typ := wasm.ExternRef
		if op == 27 {
			typ = wasm.AnyRef
		}
		body := []byte{2, 1, 0x63, 0, 1, 0x7b, 0x41, 0xc9, 0, 0xfb, 0, 0, 0x21, 1, 0xfd, 0x0c}
		body = append(body, payload...)
		body = append(body, 0x20, 0, 0xfb, op, 0xd1, 0x1a, 0x21, 2)
		for i := 0; i < 3; i++ {
			body = append(body, 0xfb, 1, 0, 0x24, 0)
		}
		body = append(body, 0x20, 2, 0xfd, 0x1d, 0, 0x20, 2, 0xfd, 0x1d, 1, 0x20, 1, 0xfb, 2, 0, 0, 0x0b)
		body = append(wasmtest.ULEB(uint32(len(body))), body...)
		data := wasmtest.Module(
			wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{wasm.I64, wasm.I64, wasm.I32}))),
			wasmtest.Section(3, wasmtest.Vec([]byte{1})),
			wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
			wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
			wasmtest.Section(10, wasmtest.Vec(body)),
		)
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			got, err := in.Invoke("run", 0)
			if err != nil || len(got) != 3 || got[0] != binary.LittleEndian.Uint64(payload) || got[1] != binary.LittleEndian.Uint64(payload[8:]) || got[2] != 73 {
				t.Fatalf("op%d vector and local: %x,%v", op, got, err)
			}
		}
		in.Close()
		c.Close()
	}
}

func BenchmarkGCExternLocalRootsCompile(b *testing.B) {
	for _, op := range []byte{26, 27} {
		b.Run(fmt.Sprintf("op%d", op), func(b *testing.B) {
			data := gcExternLocalRootsModule(op)
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, err := Compile(cfg, data)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(c.CodeSize()), "native-B")
				b.ReportMetric(float64(c.GCNativeRootAdmission().MetadataBytes), "root-B")
				c.Close()
			}
		})
	}
}

// Baseline execution is incorrect. wrong/op makes that limit visible in raw
// cost samples; its timing is not evidence for a correct alternative.
func BenchmarkGCExternLocalRoots(b *testing.B) {
	for _, op := range []byte{26, 27} {
		b.Run(fmt.Sprintf("op%d", op), func(b *testing.B) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcExternLocalRootsModule(op))
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			wrong := 0
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := in.Invoke("run", 0)
				if err != nil || len(got) != 1 {
					b.Fatalf("run:%v,%v", got, err)
				}
				if got[0] != 73 {
					wrong++
				}
			}
			b.ReportMetric(float64(wrong)/float64(b.N), "wrong/op")
		})
	}
}

func TestGCExternConversionForeignLocalRoots(t *testing.T) {
	body := []byte{1, 1, 0x6e, 0x20, 0, 0xfb, 26, 0x21, 1}
	for i := 0; i < 3; i++ {
		body = append(body, 0xfb, 1, 0, 0x24, 0)
	}
	body = append(body, 0x20, 1, 0xfb, 27, 0x0b)
	body = append(wasmtest.ULEB(uint32(len(body))), body...)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.ExternRef}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(body)),
	)
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	foreign, err := in.NewExternRef("foreign-local")
	if err != nil {
		t.Fatal(err)
	}
	word := ValueExternRef(foreign).Bits()
	for i := 0; i < 3; i++ {
		got, err := in.Invoke("run", word)
		if err != nil || len(got) != 1 || got[0] != word {
			t.Fatalf("foreign local: %x,%v; want%x", got, err, word)
		}
		if err := in.CollectGC(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGCExternConversionCheckedTableRoots(t *testing.T) {
	body := []byte{1, 1, 0x63, 0, 0x41, 0xc9, 0, 0xfb, 0, 0, 0x21, 1, 0x41, 0, 0x20, 1, 0x26, 0, 0x41, 1, 0x20, 0, 0xfb, 26, 0x26, 0}
	for i := 0; i < 3; i++ {
		body = append(body, 0xfb, 1, 0, 0x24, 0)
	}
	body = append(body, 0x41, 0, 0x25, 0, 0xfb, 22, 0, 0xfb, 2, 0, 0, 0x41, 1, 0x25, 0, 0xfb, 27, 0x0b)
	body = append(wasmtest.ULEB(uint32(len(body))), body...)
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.I32, wasm.ExternRef}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(4, wasmtest.Vec([]byte{0x6e, 0, 2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(body)),
	)
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	foreign, err := in.NewExternRef("foreign-table")
	if err != nil {
		t.Fatal(err)
	}
	word := ValueExternRef(foreign).Bits()
	for i := 0; i < 3; i++ {
		got, err := in.Invoke("run", word)
		if err != nil || len(got) != 2 || got[0] != 73 || got[1] != word {
			t.Fatalf("table roots: %x,%v", got, err)
		}
		state := in.existingGCRefTestTableState()
		if state == nil || state.Count != 2 || state.Conversion == nil {
			t.Fatal("missing checked table owner")
		}
		object, err := in.gc.CheckedTableSlot(state.Slots[0])
		if err != nil || !object.IsObj() {
			t.Fatalf("object root:%x,%v", object, err)
		}
		nonObject, err := in.gc.CheckedTableSlot(state.Slots[1])
		if err != nil || !nonObject.IsNull() {
			t.Fatalf("foreign collector root:%x,%v", nonObject, err)
		}
		anyWord := binary.LittleEndian.Uint64(state.Descriptor[16:24])
		known, err := state.Conversion.isForeignAny(anyWord)
		if err != nil || !known {
			t.Fatalf("foreign identity owner:%v,%v", known, err)
		}
		if err := in.CollectGC(); err != nil {
			t.Fatal(err)
		}
		field, err := in.gc.StructGet(object, 0)
		if err != nil || field.I32() != 73 {
			t.Fatalf("checked object lost:%v,%v", field, err)
		}
		known, err = state.Conversion.isForeignAny(anyWord)
		if err != nil || !known {
			t.Fatalf("foreign identity lost:%v,%v", known, err)
		}
	}
}

func TestGCExternConversionMissingRootsDisableCollection(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcExternLocalRootsModule(26))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.GCNativeRootAdmission().Exact {
		t.Fatal("fixture has no exact roots")
	}
	c.validateMemo.gcFrameRoots = nil
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.gc.CollectsOnAllocation() {
		t.Fatal("missing required roots still allow native allocation to collect")
	}
	got, err := in.Invoke("run", 0)
	if err != nil || len(got) != 1 || got[0] != 73 {
		t.Fatalf("stable fallback:%v,%v", got, err)
	}
	if err := in.CollectGC(); err != nil {
		t.Fatal(err)
	}
}

func TestGCExternConversionArtifactRootAdmission(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcExternLocalRootsModule(26))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.GCNativeRootAdmission().Exact {
		t.Fatal("fixture has no exact roots")
	}
	blob, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if loaded != nil {
		defer loaded.Close()
	}
	if err == nil {
		t.Fatal("codec admitted an extern-conversion product without persisted bridge ownership")
	}
	if !strings.Contains(err.Error(), "required feature") && !strings.Contains(err.Error(), "GC frame-root metadata requires exact native GC roots") {
		t.Fatalf("unexpected artifact rejection:%v", err)
	}
	t.Logf("extern bridge artifact stays fail-closed:%v", err)
}

func TestGCExternConversionWithoutHeapKeepsTinyProfile(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.ExternRef}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xfb, 26, 0xfb, 27, 0x0b}))),
	)
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 16}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if in.gc.Profile() != GCProfileTiny {
		t.Fatalf("object-free bridge changed collector profile:%v", in.gc.Profile())
	}
	if roots := c.GCNativeRootAdmission(); !roots.Required || !roots.Exact || roots.Safepoints != 0 || roots.Callsites != 0 || roots.MaximumRoots != 0 {
		t.Fatalf("noncollecting bridge lacks an exact empty graph: %+v", roots)
	}
	for i := 0; i < 3; i++ {
		got, err := in.Invoke("run", 0)
		if err != nil || len(got) != 1 || got[0] != 0 {
			t.Fatalf("null bridge:%v,%v", got, err)
		}
	}
}

func TestGCExternConversionRejectsForgedFrameWords(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcExternLocalRootsModule(26))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	other, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	foreign, err := in.NewExternRef("owned-frame")
	if err != nil {
		t.Fatal(err)
	}
	public := ValueExternRef(foreign).Bits()
	known, err := in.existingGCRefTestTableState().Conversion.anyFromExtern(public)
	if err != nil {
		t.Fatal(err)
	}
	visit := func(owner *Instance, word uint64) (refs []gc.Ref, panicValue any) {
		frame := []uint64{word}
		roots := gcNativeFrameRoots{owner: owner, base: uintptr(unsafe.Pointer(&frame[0])), offsets: []uint32{0}, frameBytes: 8}
		defer func() { panicValue = recover(); goruntime.KeepAlive(frame) }()
		roots.RangeRoots(func(slot gc.RootSlot) bool { refs = append(refs, slot.GetRef()); return true })
		return
	}
	if refs, p := visit(in, known); p != nil || len(refs) != 0 {
		t.Fatalf("owned foreign value exposed to collector:%x,%v", refs, p)
	}
	if refs, p := visit(in, uint64(gc.I31New(7))); p != nil || len(refs) != 1 || refs[0] != gc.I31New(7) {
		t.Fatalf("compact root:%x,%v", refs, p)
	}
	for _, word := range []uint64{1 << 32, 1<<32 | 1, ^uint64(0), public} {
		if _, p := visit(in, word); p == nil {
			t.Fatalf("accepted unknown frame word:%x", word)
		}
	}
	if _, p := visit(other, known); p == nil {
		t.Fatal("accepted another instance's foreign frame identity")
	}
}

func gcExternForeignRootModule(localsN int, dataRoundtrip bool) []byte {
	params := make([]wasm.ValType, localsN)
	for i := range params {
		params[i] = wasm.ExternRef
	}
	body := append([]byte{1}, wasmtest.ULEB(uint32(localsN))...)
	body = append(body, 0x6e)
	for i := 0; i < localsN; i++ {
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(uint32(i))...)
		body = append(body, 0xfb, 26, 0x21)
		body = append(body, wasmtest.ULEB(uint32(localsN+i))...)
	}
	if dataRoundtrip {
		body = append(body, 0xfb, 1, 0, 0xfb, 27, 0xfb, 26, 0x1a)
	}
	for i := 0; i < 3; i++ {
		body = append(body, 0xfb, 1, 0, 0x24, 0)
	}
	for i := 0; i < localsN; i++ {
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(uint32(localsN+i))...)
		body = append(body, 0xfb, 27)
	}
	body = append(body, 0x0b)
	body = append(wasmtest.ULEB(uint32(len(body))), body...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType(params, params), wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.ExternRef}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("bridge", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(body, wasmtest.Code([]byte{0x20, 0, 0xfb, 26, 0xfb, 27, 0x0b}))),
	)
}

func gcExternForeignRootInputs(t testing.TB, in *Instance, entries, localsN int) []uint64 {
	t.Helper()
	words := make([]uint64, entries)
	for i := range words {
		ref, err := in.NewExternRef(i)
		if err != nil {
			t.Fatal(err)
		}
		words[i] = ValueExternRef(ref).Bits()
		got, err := in.Invoke("bridge", words[i])
		if err != nil || len(got) != 1 || got[0] != words[i] {
			t.Fatalf("bridge:%x,%v", got, err)
		}
	}
	return words[entries-localsN:]
}

func TestGCExternConversionManyForeignLocals(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcExternForeignRootModule(8, true))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 512, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	args := gcExternForeignRootInputs(t, in, 32, 8)
	if roots := c.GCNativeRootAdmission(); !roots.Exact || roots.MaximumRoots < 8 {
		t.Fatalf("multiple live roots not admitted:%+v", roots)
	}
	for i := 0; i < 3; i++ {
		got, err := in.Invoke("run", args...)
		if err != nil || !slices.Equal(got, args) {
			t.Fatalf("foreign locals:%x,%v; want%x", got, err, args)
		}
		if err := in.CollectGC(); err != nil {
			t.Fatal(err)
		}
	}
}

func BenchmarkGCExternForeignRoots(b *testing.B) {
	for _, localsN := range []int{1, 8} {
		for _, entries := range []int{8, 256} {
			b.Run(fmt.Sprintf("locals=%d/entries=%d", localsN, entries), func(b *testing.B) {
				c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcExternForeignRootModule(localsN, false))
				if err != nil {
					b.Fatal(err)
				}
				defer c.Close()
				in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 512, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
				if err != nil {
					b.Fatal(err)
				}
				defer in.Close()
				args := gcExternForeignRootInputs(b, in, entries, localsN)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					got, err := in.Invoke("run", args...)
					if err != nil || !slices.Equal(got, args) {
						b.Fatalf("run:%x,%v", got, err)
					}
				}
				b.StopTimer()
				if count := in.existingGCRefTestTableState().Conversion.count; count != uint32(entries) {
					b.Fatalf("entry count changed:%d", count)
				}
			})
		}
	}
}

func TestGCExternConversionVoidImportCannotUseEmptyRoots(t *testing.T) {
	producerData := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 2, 0x7f, 1, 0x7e, 1}, wasmtest.FuncType(nil, nil))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("tick", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0xfb, 1, 0, 0x24, 0, 0xfb, 1, 0, 0x24, 0, 0xfb, 1, 0, 0x24, 0, 0x0b}))),
	)
	receiverBody := []byte{1, 1, 0x6e, 0x20, 0, 0xfb, 26, 0x21, 1, 0x10, 0, 0x20, 1, 0xfb, 27, 0x0b}
	receiverBody = append(wasmtest.ULEB(uint32(len(receiverBody))), receiverBody...)
	receiverData := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.ExternRef}, []wasm.ValType{wasm.ExternRef}), wasmtest.FuncType(nil, nil))),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("tick")...), 0, 1))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(receiverBody)),
	)
	cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rt.Close()
	pc, err := Compile(cfg, producerData)
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	pm, err := rt.Module(pc)
	if err != nil {
		t.Fatal(err)
	}
	defer pm.Close()
	producer, err := rt.Instantiate(context.Background(), pm, WithGC(GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 512, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}))
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	tick, err := producer.ExportedFunc("tick")
	if err != nil {
		t.Fatal(err)
	}
	rc, err := Compile(cfg, receiverData)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	rm, err := rt.Module(rc)
	if err != nil {
		t.Fatal(err)
	}
	defer rm.Close()
	receiver, err := rt.Instantiate(context.Background(), rm, WithImports(testImports("env.tick", tick)), WithGC(GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 16}))
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	admission := rc.GCNativeRootAdmission()
	if !admission.Required {
		t.Fatal("void import removed the broad extern root requirement")
	}
	if admission.Exact && admission.Callsites == 0 {
		t.Fatalf("collecting imported function received an empty graph:%+v", admission)
	}
	if !admission.Exact && receiver.gc.CollectsOnAllocation() {
		t.Fatal("unproven imported call permits native collection")
	}
	foreign, err := receiver.NewExternRef("live across void GC call")
	if err != nil {
		t.Fatal(err)
	}
	word := ValueExternRef(foreign).Bits()
	before := producer.gc.Stats().FullCollections
	for i := 0; i < 3; i++ {
		got, err := receiver.Invoke("run", word)
		if err != nil || len(got) != 1 || got[0] != word {
			t.Fatalf("foreign local across collecting import:%x,%v", got, err)
		}
	}
	if producer.gc.Stats().FullCollections <= before {
		t.Fatal("void import did not collect")
	}
}
