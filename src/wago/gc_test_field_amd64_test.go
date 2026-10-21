//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"unsafe"

	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
)

func TestGCTestField(t *testing.T) {
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-test-field.wasm")
	if err != nil {
		t.Fatal(err)
	}
	for _, codec := range []bool{false, true} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
		if err != nil {
			t.Fatal(err)
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
			for _, setup := range []string{"init", "wrong", "clear", "i31"} {
				if _, e = in.Invoke(setup); e != nil {
					t.Fatal(e)
				}
				want := uint64(99)
				if setup == "init" {
					want = 7
				}
				for _, entry := range []string{"read-global", "nop", "different-local"} {
					got, e := in.Invoke(entry)
					if e != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("codec=%v %s/%s=%v,%v want%d", codec, setup, entry, got, e, want)
					}
				}
				wide := uint64(99)
				if setup == "init" {
					wide = 9
				}
				if got, e := in.Invoke("i64"); e != nil || len(got) != 1 || got[0] != wide {
					t.Fatalf("%s/i64=%v,%v", setup, got, e)
				}
			}
			for _, tc := range []struct {
				name string
				want uint64
			}{{"packed-s", 0xfffffffe}, {"packed-u", 65533}, {"nonfinal", 5}} {
				got, e := in.Invoke(tc.name)
				if e != nil || len(got) != 1 || got[0] != tc.want {
					t.Fatalf("%s=%v,%v want%d", tc.name, got, e, tc.want)
				}
			}
			if _, e = in.Invoke("init"); e != nil {
				t.Fatal(e)
			}
			if got, e := in.Invoke("loop", 8192); e != nil || len(got) != 1 || got[0] != 57344 {
				t.Fatalf("loop=%v,%v", got, e)
			}
			if got, e := in.Invoke("pressure", 1, 2, 3, 4, 5, 6, 7, 8); e != nil || len(got) != 1 || got[0] != 36 {
				t.Fatalf("pressure=%v,%v", got, e)
			}
			if got, e := in.Invoke("stack-root"); e != nil || len(got) != 1 || got[0] != 17 {
				t.Fatalf("older stack root=%v,%v", got, e)
			}
			if _, e = in.Invoke("init"); e != nil {
				t.Fatal(e)
			}
			if got, e := in.Invoke("root"); e != nil || len(got) != 1 || got[0] != 17 {
				t.Fatalf("retained child=%v,%v", got, e)
			}
			if e = in.CollectGC(); e != nil {
				t.Fatal(e)
			}
			in.Close()
		}
		c.Close()
	}
}

func TestGCTestFieldConditionalExtent(t *testing.T) {
	data, e := os.ReadFile("../../tests/fixtures/wasm/gc-test-field.wasm")
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
	for ai, artifact := range []*Compiled{c, loaded} {
		for _, setup := range []string{"init", "wrong"} {
			for _, kind := range []string{"header", "handle", "space", "interval", "high-bits"} {
				t.Run(fmt.Sprintf("artifact%d/%s/%s", ai, setup, kind), func(t *testing.T) {
					in, e := Instantiate(artifact)
					if e != nil {
						t.Fatal(e)
					}
					defer in.Close()
					if _, e = in.Invoke(setup); e != nil {
						t.Fatal(e)
					}
					bits := readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)
					ref := gc.Ref(uint32(bits))
					v := in.gc.NativeView()
					h := unsafe.Slice((*byte)(offHeapPtr(v.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
					header := unsafe.Slice((*byte)(offHeapPtr(v.Spaces[h[gc.NativeHandleSpaceOffset]].Base+uintptr(binary.LittleEndian.Uint32(h)))), gc.HeaderSize)
					var restore func()
					switch kind {
					case "header", "handle":
						target := header[4:]
						if kind == "handle" {
							target = h[gc.NativeHandleSizeOffset:]
						}
						old := binary.LittleEndian.Uint32(target)
						binary.LittleEndian.PutUint32(target, gc.HeaderSize)
						restore = func() { binary.LittleEndian.PutUint32(target, old) }
					case "space":
						old := h[gc.NativeHandleSpaceOffset]
						h[gc.NativeHandleSpaceOffset] = 255
						restore = func() { h[gc.NativeHandleSpaceOffset] = old }
					case "interval":
						actual, target := uint32(in.requireGCDomainType(2)), uint32(in.requireGCDomainType(0))
						if actual == target || actual >= v.SubtypeIntervalCount || target >= v.SubtypeIntervalCount {
							t.Fatalf("bad fixture canonical IDs %d/%d, count%d", actual, target, v.SubtypeIntervalCount)
						}
						words := unsafe.Slice((*uint64)(offHeapPtr(v.SubtypeIntervals)), int(v.SubtypeIntervalCount))
						old := words[actual]
						words[actual] = words[target]
						restore = func() { words[actual] = old }
					case "high-bits":
						writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, bits|uint64(1)<<32)
						restore = func() { writeGlobalObject(in.globalCells[0], in.c.Globals[0].Type, bits) }
					}
					got, err := in.Invoke("read-global")
					restore()
					if kind == "high-bits" {
						// Public invocation checks reference globals before native entry. Keep
						// that diagnostic visible; this does not claim native-leaf coverage.
						if err == nil || !strings.Contains(err.Error(), "non-compact reference") {
							t.Fatalf("invalid reference boundary=%v want non-compact reference diagnostic", err)
						}
						return
					}
					falseResult := setup == "wrong" && kind != "space" && kind != "interval"
					success := kind == "interval" && setup == "init"
					if falseResult || success {
						want := uint64(99)
						if success {
							want = 7
						}
						if err != nil || len(got) != 1 || got[0] != want {
							t.Fatalf("result=%v,%v want%d", got, err, want)
						}
					} else {
						var trap *TrapError
						if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
							t.Fatalf("trap=%v want cast failure", err)
						}
					}
				})
			}
		}
	}
}

func TestGCTestFieldNullableFallback(t *testing.T) {
	data, e := os.ReadFile("../../tests/fixtures/wasm/gc-test-field.wasm")
	if e != nil {
		t.Fatal(e)
	}
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	in, e := Instantiate(c)
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	for _, setup := range []string{"init", "wrong", "i31", "clear"} {
		if _, e = in.Invoke(setup); e != nil {
			t.Fatal(e)
		}
		got, err := in.Invoke("nullable")
		if setup == "clear" {
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
				t.Fatalf("nullable null=%v want cast failure", err)
			}
		} else {
			want := uint64(99)
			if setup == "init" {
				want = 7
			}
			if err != nil || len(got) != 1 || got[0] != want {
				t.Fatalf("nullable/%s=%v,%v want%d", setup, got, err, want)
			}
		}
	}
}

func BenchmarkGCTestField(b *testing.B) {
	data, e := os.ReadFile("../../tests/fixtures/wasm/gc-test-field.wasm")
	if e != nil {
		b.Fatal(e)
	}
	for _, entry := range []string{"loop", "loop-nop", "loop-wrong"} {
		b.Run(entry, func(b *testing.B) {
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
			setup := "init"
			if entry == "loop-wrong" {
				setup = "wrong"
			}
			if _, e = in.Invoke(setup); e != nil {
				b.Fatal(e)
			}
			invoke := entry
			want := uint64(57344)
			if entry == "loop-wrong" {
				invoke = "loop"
				want = 99 * 8192
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				got, e := in.Invoke(invoke, 8192)
				if e != nil || len(got) != 1 || got[0] != want {
					b.Fatalf("%s=%v,%v", entry, got, e)
				}
			}
		})
	}
}

func TestGCTestFieldDirtyPins(t *testing.T) {
	data, e := os.ReadFile("../../tests/fixtures/wasm/gc-test-field.wasm")
	if e != nil {
		t.Fatal(e)
	}
	c, e := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	// This body alone uses six global pins, including R9-R11, and one object
	// local. RBP remains free for the conditional field-address certificate.
	index := c.Exports["dirty"] - c.NumImports
	start, end := min(c.Entry[index], c.InternalEntry[index]), min(c.Entry[index+1], c.InternalEntry[index+1])
	if start < 0 || end <= start || end > len(c.code) {
		t.Fatalf("invalid dirty function native span %d..%d", start, end)
	}
	if n := bytes.Count(c.code[start:end], []byte{0x41, 0x51, 0x41, 0x52, 0x41, 0x53}); n != 1 {
		t.Fatalf("R9-R11 save sequences=%d want1", n)
	}
	loaded := publicArtifactRoundTrip(t, c)
	defer loaded.Close()
	for ai, artifact := range []*Compiled{c, loaded} {
		for _, kind := range []string{"valid", "wrong", "null", "short"} {
			t.Run(fmt.Sprintf("artifact%d/%s", ai, kind), func(t *testing.T) {
				in, e := Instantiate(artifact)
				if e != nil {
					t.Fatal(e)
				}
				defer in.Close()
				setup := "init"
				if kind == "wrong" {
					setup = "wrong"
				} else if kind == "null" {
					setup = "clear"
				}
				if _, e = in.Invoke(setup); e != nil {
					t.Fatal(e)
				}
				var restore func()
				if kind == "short" {
					ref := gc.Ref(uint32(readGlobalObject(in.globalCells[0], in.c.Globals[0].Type)))
					v := in.gc.NativeView()
					h := unsafe.Slice((*byte)(offHeapPtr(v.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
					header := unsafe.Slice((*byte)(offHeapPtr(v.Spaces[h[gc.NativeHandleSpaceOffset]].Base+uintptr(binary.LittleEndian.Uint32(h)))), gc.HeaderSize)
					old := binary.LittleEndian.Uint32(header[4:])
					binary.LittleEndian.PutUint32(header[4:], gc.HeaderSize)
					restore = func() { binary.LittleEndian.PutUint32(header[4:], old) }
				}
				got, err := in.Invoke("dirty")
				if restore != nil {
					restore()
				}
				if kind == "short" {
					var trap *TrapError
					if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
						t.Fatalf("trap=%v want cast failure", err)
					}
				} else {
					want := uint64(99)
					if kind == "valid" {
						want = 7
					}
					if err != nil || len(got) != 1 || got[0] != want {
						t.Fatalf("result=%v,%v want%d", got, err, want)
					}
				}
				for g := 0; g < 6; g++ {
					if got := readGlobalObject(in.globalCells[g+2], in.c.Globals[g+2].Type); got != uint64(g+1) {
						t.Errorf("global%d=%#x want%d", g, got, g+1)
					}
				}
			})
		}
	}
}
