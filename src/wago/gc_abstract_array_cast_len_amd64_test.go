//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"os"
	"testing"
	"unsafe"
)

func abstractArrayCastLenModule(t testing.TB) *Compiled {
	t.Helper()
	requireCompleteCore3Backend(t)
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-abstract-array-cast-len.wasm")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestGCAbstractArrayCastLen(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	c := abstractArrayCastLenModule(t)
	for _, cfg := range []GCConfig{{VerifyAfterCollect: true}, {CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyHeapBytes: 1 << 20, VerifyAfterCollect: true}} {
		in, err := Instantiate(c, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer in.Close()
			call := func(name string, want uint64, args ...uint64) {
				t.Helper()
				got, err := in.Invoke(name, args...)
				if err != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("%s(%v)=%v,%v; want [%d]", name, args, got, err, want)
				}
			}
			setup := func(name string, args ...uint64) {
				t.Helper()
				if _, err := in.Invoke(name, args...); err != nil {
					t.Fatal(err)
				}
			}
			trap := func(name string, want TrapCode, args ...uint64) {
				t.Helper()
				_, err := in.Invoke(name, args...)
				var got *TrapError
				if !errors.As(err, &got) || got.Code != want {
					t.Fatalf("%s(%v)=%v; want trap %v", name, args, err, want)
				}
			}
			for _, init := range []string{"init", "init-vector", "init-refs"} {
				for _, n := range []uint64{0, 1, 64, 4096} {
					setup(init, n)
					for i := 0; i < 3; i++ {
						call("length", n)
						call("nullable-length", n)
						call("pressure", n+24*17+276, 17)
						if err := in.CollectGC(); err != nil {
							t.Fatal(err)
						}
					}
					// A zero optional encoding requests the legacy checked Go path.
					in.gc.NativeView().SubtypeEncoding = 0
					call("length", n)
					call("pressure", n+24*17+276, 17)
				}
			}
			setup("clear")
			trap("length", TrapCastFailure)
			trap("pressure", TrapCastFailure, 17)
			trap("nullable-length", TrapNullReference)
			trap("before-length", TrapDivZero, 0)
			setup("other")
			trap("pressure", TrapCastFailure, 17)
			trap("length", TrapCastFailure)
			trap("nullable-length", TrapCastFailure)
			// An old ABI-9 runtime has plain timestamps and a zero encoding word.
			// Final struct roots have odd plain entry timestamps. Ignoring the
			// encoding guard would wrongly accept this struct as an array.
			func() {
				view := in.gc.NativeView()
				words := unsafe.Slice((*uint64)(offHeapPtr(view.SubtypeIntervals)), int(view.SubtypeIntervalCount))
				saved := append([]uint64(nil), words...)
				defer func() { copy(words, saved); view.SubtypeEncoding = gc.NativeSubtypeEncodingKindBit }()
				for i, word := range words {
					words[i] = uint64(uint32(word>>32)>>1)<<32 | uint64(uint32(word)>>1)
				}
				view.SubtypeEncoding = 0
				trap("length", TrapCastFailure)
			}()
			setup("i31")
			trap("length", TrapCastFailure)
			foreign, err := in.NewExternRef("not an array")
			if err != nil {
				t.Fatal(err)
			}
			trap("foreign", TrapCastFailure, foreign.token)
		}()
	}
}

func TestGCAbstractArrayCastLenKeepsAllocationBatch(t *testing.T) {
	c := abstractArrayCastLenModule(t)
	for _, tc := range []struct {
		name string
		n    uint64
	}{{"empty", 0}, {"populated", 64}} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{NurseryBytes: 1 << 20, VerifyAfterCollect: true}})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if _, err := in.Invoke("init", tc.n); err != nil {
				t.Fatal(err)
			}
			got, err := in.Invoke("allocate-across-length", 2)
			if err != nil || len(got) != 1 || got[0] != 2*tc.n+1 {
				t.Fatalf("allocation loop=%v,%v; want [%d]", got, err, 2*tc.n+1)
			}
			state := unsafe.Slice((*byte)(offHeapPtr(in.gc.NativeView().StructAllocState)), gc.NativeStructAllocStateSize)
			if cursor := binary.LittleEndian.Uint32(state[gc.NativeStructAllocCursorOffset:]); cursor != 1 {
				t.Fatalf("native batch cursor=%d, want 1; readonly array cast/length disturbed its allocation batch", cursor)
			}
			if err := in.CollectGC(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func BenchmarkGCAbstractArrayCastLen(b *testing.B) {
	if !hostSupportsSIMD() {
		b.Skip("host SIMD unavailable")
	}
	c := abstractArrayCastLenModule(b)
	for _, name := range []string{"loop", "allocate-across-length"} {
		b.Run(name, func(b *testing.B) {
			in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{NurseryBytes: 1 << 20, ThroughputHeapBytes: 64 << 20}})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			if _, err := in.Invoke("init", 64); err != nil {
				b.Fatal(err)
			}
			const n = 8192
			want := uint64(64 * n)
			if name == "allocate-across-length" {
				want += n * (n - 1) / 2
			}
			invoke := func() {
				got, err := in.Invoke(name, n)
				if err != nil || len(got) != 1 || got[0] != want {
					b.Fatalf("%s=%v,%v; want [%d]", name, got, err, want)
				}
			}
			invoke()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				invoke()
			}
		})
	}
}

func TestGCAbstractArrayCastLenSpaceByte(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	c := abstractArrayCastLenModule(t)
	if status := c.GCNativeRootAdmission(); !status.Required || !status.Exact {
		t.Fatalf("fixture requires exact root admission: %+v", status)
	}
	loaded := publicArtifactRoundTrip(t, c)
	defer loaded.Close()
	for artifactIndex, artifact := range []*Compiled{c, loaded} {
		for _, tc := range []struct {
			space byte
			cfg   GCConfig
		}{
			{gc.NativeSpaceNursery, GCConfig{NurseryBytes: 1 << 20}},
			{gc.NativeSpaceOld, GCConfig{NurseryBytes: 1 << 20}},
			{gc.NativeSpaceLarge, GCConfig{LargeObjectBytes: 16}},
			{gc.NativeSpaceTiny, GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 1 << 20, TinyBlockBytes: 16}},
		} {
			t.Run(fmt.Sprintf("saved%d/space%d", artifactIndex, tc.space), func(t *testing.T) {
				in, err := Instantiate(artifact, InstantiateOptions{GC: tc.cfg})
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if in.c.genericGCFrameRoots() == nil || !in.gc.CollectsOnAllocation() {
					t.Fatal("fixture must reach the requested collector without boundary collection")
				}
				if _, err := in.Invoke("init", 64); err != nil {
					t.Fatal(err)
				}
				ref := uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type))
				if tc.space == gc.NativeSpaceOld {
					if err := in.gc.ForcePromote(gc.Ref(ref)); err != nil {
						t.Fatal(err)
					}
				}
				view := in.gc.NativeView()
				entry := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
				saved := append([]byte(nil), entry...)
				defer copy(entry, saved)
				if entry[gc.NativeHandleSpaceOffset] != tc.space {
					t.Fatalf("fixture space=%d, want %d", entry[gc.NativeHandleSpaceOffset], tc.space)
				}
				check := func(name string, invalid bool) {
					t.Helper()
					got, err := in.Invoke(name)
					if invalid {
						if err == nil || err.Error() != "wago: WasmGC struct helper: gc: invalid object ref" {
							t.Fatalf("entry=%x %s=%v,%v; want diagnostic invalid object ref", entry, name, got, err)
						}
					} else if err != nil || len(got) != 1 || got[0] != 64 {
						t.Fatalf("space=%d %s=%v,%v; want64", entry[gc.NativeHandleSpaceOffset], name, got, err)
					}
				}
				// The surrounding class and remembered bytes are not part of the index.
				entry[12], entry[13], entry[15] = 0xff, 0x80, 0xff
				check("length", false)
				check("nullable-length", false)
				if tc.space == gc.NativeSpaceNursery {
					for space := 0; space <= 255; space++ {
						if space >= 1 && space <= 4 {
							continue
						}
						entry[gc.NativeHandleSpaceOffset] = byte(space)
						check("length", true)
						check("nullable-length", true)
					}
				}
				if tc.space == gc.NativeSpaceNursery {
					for _, bad := range []struct {
						offset int
						value  uint32
					}{
						{gc.NativeHandleSizeOffset, gc.HeaderSize - 1},
						{gc.NativeHandleSizeOffset, ^uint32(0)},
						{gc.NativeHandleOffsetOffset, ^uint32(0)},
					} {
						copy(entry, saved)
						binary.LittleEndian.PutUint32(entry[bad.offset:], bad.value)
						check("length", true)
						check("nullable-length", true)
					}
				}
				copy(entry, saved)
				check("length", false)
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
				check("nullable-length", false)
			})
		}
	}
}
