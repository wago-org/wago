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

func gcReadRegionsCompiled(t testing.TB) *Compiled {
	t.Helper()
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-read-regions.wasm")
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

func TestGCReadRegions(t *testing.T) {
	c := gcReadRegionsCompiled(t)
	loaded := publicArtifactRoundTrip(t, c)
	defer loaded.Close()
	for _, artifact := range []*Compiled{c, loaded} {
		if a := artifact.GCNativeRootAdmission(); !a.Required || !a.Exact {
			t.Fatalf("root admission=%+v", a)
		}
		for _, cfg := range []GCConfig{{VerifyAfterCollect: true}, {CollectEveryAlloc: true, ForceMajorEveryMinor: true, StressNurseryBytes: 128, VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}} {
			in, e := Instantiate(artifact, InstantiateOptions{GC: cfg})
			if e != nil {
				t.Fatal(e)
			}
			setup := func(name string) {
				t.Helper()
				if _, e := in.Invoke(name); e != nil {
					t.Fatal(e)
				}
			}
			setup("init")
			setup("init-second")
			for _, tc := range []struct {
				name string
				args []uint64
				want uint64
			}{{"read", []uint64{0}, 7}, {"read", []uint64{15}, 7}, {"loop", []uint64{8192}, 57344}, {"pressure-loop", []uint64{1, 2, 3, 4, 5, 6, 7, 8, 8192}, 57380}, {"branch", []uint64{0}, 17}, {"branch", []uint64{2}, 23}, {"forward", []uint64{15}, 7}, {"arms", []uint64{0}, 32}, {"arms", []uint64{1}, 23}, {"nested", []uint64{0}, 17}, {"nested", []uint64{1}, 23}, {"nested", []uint64{2}, 17}, {"late", []uint64{0}, 23}, {"join", nil, 19}, {"pressure", []uint64{1, 2, 3, 4, 5, 6, 7, 8}, 43}, {"two-targets", nil, 24}, {"different", nil, 58}, {"pressure-root", []uint64{1, 2, 3, 4, 5, 6, 7, 8}, 53}, {"many", nil, 119}, {"home-branch", []uint64{1, 2, 3, 4, 5, 6, 7, 8}, 151}} {
				if tc.name == "pressure-root" {
					setup("init")
				}
				if got, e := in.Invoke(tc.name, tc.args...); e != nil || len(got) != 1 || got[0] != tc.want {
					t.Fatalf("%s=%v,%v want%d", tc.name, got, e, tc.want)
				}
			}
			for _, entry := range []string{"roots", "stack-roots"} {
				setup("init")
				want := uint64(1040)
				if entry == "stack-roots" {
					want = 1017
				}
				if got, e := in.Invoke(entry); e != nil || len(got) != 1 || got[0] != want {
					t.Fatalf("%s=%v,%v want%d", entry, got, e, want)
				}
			}

			setup("init")
			_, trapErr := in.Invoke("after-trap")
			var afterTrap *TrapError
			if !errors.As(trapErr, &afterTrap) || afterTrap.Code != TrapUnreachable {
				t.Fatalf("after-trap=%v", trapErr)
			}
			for _, tc := range []struct {
				setup string
				index uint64
				trap  TrapCode
			}{{"init", 16, TrapUnreachable}, {"init", 0xffffffff, TrapUnreachable}, {"wrong", 0, TrapCastFailure}, {"null", 0, TrapCastFailure}, {"missing", 16, TrapUnreachable}, {"missing", 0, TrapCastFailure}, {"short", 1, TrapBuiltin}} {
				setup(tc.setup)
				_, e := in.Invoke("read", tc.index)
				var trap *TrapError
				if !errors.As(e, &trap) || trap.Code != tc.trap {
					t.Fatalf("%s/%d=%v want%d", tc.setup, tc.index, e, tc.trap)
				}
			}
			in.Close()
		}
	}
}

func BenchmarkGCReadRegions(b *testing.B) {
	for _, entry := range []string{"loop", "pressure-loop"} {
		b.Run(entry, func(b *testing.B) {
			c := gcReadRegionsCompiled(b)
			in, e := Instantiate(c)
			if e != nil {
				b.Fatal(e)
			}
			defer in.Close()
			if _, e = in.Invoke("init"); e != nil {
				b.Fatal(e)
			}
			args := []uint64{8192}
			want := uint64(57344)
			if entry == "pressure-loop" {
				args = []uint64{1, 2, 3, 4, 5, 6, 7, 8, 8192}
				want += 36
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				got, e := in.Invoke(entry, args...)
				if e != nil || len(got) != 1 || got[0] != want {
					b.Fatalf("%s=%v,%v want%d", entry, got, e, want)
				}
			}
		})
	}
}

// The first backing-field check proves 20 bytes. The later length field needs
// 24 bytes. Do not enlarge the first check or carry a sibling-arm proof.
func TestGCReadRegionsConditionalExtent(t *testing.T) {
	c := gcReadRegionsCompiled(t)
	loaded := publicArtifactRoundTrip(t, c)
	defer loaded.Close()
	for ai, artifact := range []*Compiled{c, loaded} {
		for _, kind := range []string{"header", "handle"} {
			t.Run(fmt.Sprintf("artifact%d/%s", ai, kind), func(t *testing.T) {
				in, e := Instantiate(artifact)
				if e != nil {
					t.Fatal(e)
				}
				defer in.Close()
				if _, e = in.Invoke("init"); e != nil {
					t.Fatal(e)
				}
				bits := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)
				v := in.gc.NativeView()
				ref := gc.Ref(uint32(bits))
				h := unsafe.Slice((*byte)(offHeapPtr(v.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
				header := unsafe.Slice((*byte)(offHeapPtr(v.Spaces[h[gc.NativeHandleSpaceOffset]].Base+uintptr(binary.LittleEndian.Uint32(h)))), gc.HeaderSize)
				field := header[4:]
				if kind == "handle" {
					field = h[gc.NativeHandleSizeOffset:]
				}
				old := binary.LittleEndian.Uint32(field)
				binary.LittleEndian.PutUint32(field, 20)
				defer binary.LittleEndian.PutUint32(field, old)
				for _, tc := range []struct {
					name  string
					index uint64
					want  TrapCode
				}{{"late", 1, TrapUnreachable}, {"late", 0, TrapCastFailure}, {"arms", 0, TrapCastFailure}, {"arms", 1, TrapCastFailure}} {
					_, e = in.Invoke(tc.name, tc.index)
					var trap *TrapError
					if !errors.As(e, &trap) || trap.Code != tc.want {
						t.Fatalf("%s/%d=%v want%d", tc.name, tc.index, e, tc.want)
					}
				}
			})
		}
	}
}
