//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/binary"
	"errors"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"os"
	"testing"
	"unsafe"
)

func TestGCValueChecks(t *testing.T) {
	data, e := os.ReadFile("../../tests/fixtures/wasm/gc-value-checks.wasm")
	if e != nil {
		t.Fatal(e)
	}
	for _, codec := range []bool{false, true} {
		c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
		if e != nil {
			t.Fatal(e)
		}
		if codec {
			b, e := c.MarshalBinary()
			c.Close()
			if e != nil {
				t.Fatal(e)
			}
			c = &Compiled{}
			if e = c.UnmarshalBinary(b); e != nil {
				t.Fatal(e)
			}
		}
		if a := c.GCNativeRootAdmission(); !a.Required || !a.Exact {
			t.Fatalf("roots=%+v", a)
		}
		for _, cfg := range []GCConfig{{VerifyAfterCollect: true}, {CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, VerifyAfterCollect: true}} {
			in, e := Instantiate(c, InstantiateOptions{GC: cfg})
			if e != nil {
				t.Fatal(e)
			}
			for _, tc := range []struct {
				name string
				args []uint64
				want uint64
			}{{"run", nil, 68}, {"small", nil, 14}, {"pressure", nil, 84}, {"copy", nil, 18}, {"children", nil, 48}, {"typed-children", nil, 18}, {"test", nil, 25}, {"test-wrong", nil, 99}, {"snapshot", nil, 20}, {"later", nil, 18}, {"after-call", nil, 34}, {"branch", []uint64{0}, 11}, {"branch", []uint64{1}, 17}, {"write", nil, 17}, {"loop", []uint64{8192}, 68 * 8192}} {
				if _, e = in.Invoke("init"); e != nil {
					t.Fatal(e)
				}
				got, e := in.Invoke(tc.name, tc.args...)
				if e != nil || len(got) != 1 || got[0] != tc.want {
					t.Fatalf("codec=%v %s=%v,%v want%d", codec, tc.name, got, e, tc.want)
				}
				if e = in.CollectGC(); e != nil {
					t.Fatal(e)
				}
			}
			in.Close()
		}
		c.Close()
	}
}
func BenchmarkGCValueChecks(b *testing.B) {
	data, e := os.ReadFile("../../tests/fixtures/wasm/gc-value-checks.wasm")
	if e != nil {
		b.Fatal(e)
	}
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if e != nil {
		b.Fatal(e)
	}
	defer c.Close()
	in, e := Instantiate(c)
	if e != nil {
		b.Fatal(e)
	}
	defer in.Close()
	if _, e = in.Invoke("init"); e != nil {
		b.Fatal(e)
	}
	if got, e := in.Invoke("loop", 8192); e != nil || len(got) != 1 || got[0] != 68*8192 {
		b.Fatalf("warmup=%v,%v", got, e)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, e := in.Invoke("loop", 8192)
		if e != nil || got[0] != 68*8192 {
			b.Fatalf("loop=%v,%v", got, e)
		}
	}
}

func TestGCValueChecksExtentAndChild(t *testing.T) {
	data, e := os.ReadFile("../../tests/fixtures/wasm/gc-value-checks.wasm")
	if e != nil {
		t.Fatal(e)
	}
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	loaded := publicArtifactRoundTrip(t, c)
	defer loaded.Close()
	for _, artifact := range []*Compiled{c, loaded} {
		for _, kind := range []string{"header", "handle", "child"} {
			t.Run(kind, func(t *testing.T) {
				in, e := Instantiate(artifact)
				if e != nil {
					t.Fatal(e)
				}
				defer in.Close()
				if _, e = in.Invoke("init"); e != nil {
					t.Fatal(e)
				}
				global := 0
				if kind == "child" {
					global = 2
				}
				bits := readGlobalObject(in.globalCells[global], in.c.Globals[global].Type)
				v := in.gc.NativeView()
				h := unsafe.Slice((*byte)(offHeapPtr(v.Handles+uintptr(gc.Ref(uint32(bits))>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
				obj := unsafe.Slice((*byte)(offHeapPtr(v.Spaces[h[gc.NativeHandleSpaceOffset]].Base+uintptr(binary.LittleEndian.Uint32(h)))), 32)
				var target []byte
				if kind == "header" {
					target = obj[4:]
				} else if kind == "handle" {
					target = h[gc.NativeHandleSizeOffset:]
				} else {
					target = obj[gc.PayloadOffset+4:]
				}
				old := binary.LittleEndian.Uint32(target)
				defer binary.LittleEndian.PutUint32(target, old)
				if kind == "child" {
					binary.LittleEndian.PutUint32(target, 0xfffffff0)
					_, e = in.Invoke("children")
					var trap *TrapError
					if !errors.As(e, &trap) || trap.Code != TrapCastFailure {
						t.Fatal("checked parent incorrectly validated child")
					}
					return
				}
				binary.LittleEndian.PutUint32(target, 20)
				if got, e := in.Invoke("small"); e != nil || len(got) != 1 || got[0] != 14 {
					t.Fatalf("small read moved a later extent check: %v,%v", got, e)
				}
				_, e = in.Invoke("later")
				var trap *TrapError
				if !errors.As(e, &trap) || trap.Code != TrapCastFailure {
					t.Fatal("larger field failed to check saved handle/header limit")
				}
			})
		}
	}
}
