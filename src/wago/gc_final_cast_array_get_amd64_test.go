//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	gc "github.com/wago-org/wago/src/core/runtime/gc/native"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math"
	"os"
	"strings"
	"testing"
	"unsafe"
)

func finalCastArrayCompiled(tb testing.TB, codec bool) *Compiled {
	tb.Helper()
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-final-cast-array-get.wasm")
	if err != nil {
		tb.Fatal(err)
	}
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		tb.Fatal(err)
	}
	return c

}

func TestGCFinalCastArrayGet(t *testing.T) {
	for _, codec := range []bool{false, true} {
		c := finalCastArrayCompiled(t, codec)
		for _, cfg := range []GCConfig{{VerifyAfterCollect: true}, {CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, VerifyAfterCollect: true}} {
			in, err := Instantiate(c, InstantiateOptions{GC: cfg})
			if err != nil {
				t.Fatal(err)
			}
			func() {
				defer in.Close()
				if _, err = in.Invoke("init"); err != nil {
					t.Fatal(err)
				}
				for _, tc := range []struct {
					name string
					want uint64
				}{
					{"float32", uint64(math.Float32bits(9))}, {"int64", 1234567890123}, {"nonfinal", 17}, {"roots", 0}, {"read", 7}, {"nullable", 7}, {"packed-s", 0xfffffffe}, {"packed-u", 254}, {"half-s", 0xfffffffd}, {"half-u", 65533}, {"refs", 13}, {"float", math.Float64bits(9)}, {"vector", 13},
					{"ref-loop", 8192}, {"loop", 57344}, {"pressure", 57344}, {"negative-nop", 57344}, {"negative-index", 57344}, {"pin-entry", 7},
				} {
					var args []uint64
					if tc.name == "read" || tc.name == "nullable" {
						args = []uint64{uint64(1)<<40 | 1}
					}
					got, err := in.Invoke(tc.name, args...)
					if err != nil || len(got) != 1 || got[0] != tc.want {
						t.Fatalf("codec=%v profile=%v %s=%v,%v want%d", codec, cfg.Profile, tc.name, got, err, tc.want)
					}
					if err = in.CollectGC(); err != nil {
						t.Fatal(err)
					}
				}
				for _, setup := range []string{"init", "clear", "wrong"} {
					if _, err = in.Invoke(setup); err != nil {
						t.Fatal(err)
					}
					for _, name := range []string{"read", "nullable", "pin-entry", "index-trap"} {
						var args []uint64
						if name != "pin-entry" && name != "index-trap" {
							args = []uint64{0xffffffff}
						}
						_, err = in.Invoke(name, args...)
						want := "cast failure"
						if setup == "init" {
							if name == "pin-entry" {
								continue
							}
							want = "builtin"
							if name == "index-trap" {
								want = "integer division by zero"
							}
						}
						if setup == "clear" && name == "nullable" {
							want = "null reference"
						}
						if err == nil || !strings.Contains(err.Error(), want) {
							t.Fatalf("%s/%s error=%v want%q", setup, name, err, want)
						}
					}
				}
			}()
		}
	}
}

func BenchmarkGCFinalCastArrayGet(b *testing.B) {
	for _, name := range []string{"loop", "ref-loop", "pressure", "negative-nop", "negative-index", "pin-entry"} {
		b.Run(name, func(b *testing.B) {
			c := finalCastArrayCompiled(b, false)
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			if _, err = in.Invoke("init"); err != nil {
				b.Fatal(err)
			}
			want := uint64(57344)
			if name == "ref-loop" {
				want = 8192
			}
			if name == "pin-entry" {
				want = 7
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				got, err := in.Invoke(name)
				if err != nil || len(got) != 1 || got[0] != want {
					b.Fatalf("%s=%v,%v", name, got, err)
				}
			}
		})
	}
}

// This one-function module makes the R9-R11 value pins observable on cold exits.
func finalCastArrayPinModule() []byte {
	globals := make([][]byte, 0, 9)
	for range 8 {
		globals = append(globals, wasmtest.GlobalEntry(wasm.I64, true, []byte{0x42, 0, 0x0b}))
	}
	globals = append(globals, []byte{0x6d, 1, 0xd0, 0x6d, 0x0b})
	body := []byte{1, 1, 0x6d, 0x23, 8, 0x21, 0, 0x03, 0x40}
	for g := byte(0); g < 8; g++ {
		body = append(body, 0x23, g, 0x42, g+1, 0x7c, 0x24, g)
	}
	body = append(body, 0x0b, 0x20, 0, 0xfb, 22, 0, 0x41, 1, 0xfb, 11, 0, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5e, 0x7f, 1}, wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{1})), wasmtest.Section(6, wasmtest.Vec(globals...)), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestGCFinalCastArrayGetColdPins(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), finalCastArrayPinModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if n := bytes.Count(c.code, []byte{0x41, 0x51, 0x41, 0x52, 0x41, 0x53}); n != 1 {
		t.Fatalf("R9-R11 save groups=%d want1; pressure coverage changed", n)
	}
	t.Log("new leaf saves actual value pins R9,R10,R11; cold exits checked against all8 globals")
	for _, name := range []string{"null", "i31", "bounds", "extent", "success"} {
		t.Run(name, func(t *testing.T) {
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if name == "i31" {
				writeGlobalObject(in.globalCells[8], in.c.Globals[8].Type, 3)
			} else if name != "null" {
				length := uint32(2)
				if name == "bounds" {
					length = 1
				}
				ref, e := in.gc.NewArrayDefaultWithRoots(in.requireGCDomainType(0), length, gc.EmptyRoots{})
				if e != nil {
					t.Fatal(e)
				}
				writeGlobalObject(in.globalCells[8], in.c.Globals[8].Type, uint64(ref))
				if name == "extent" {
					view := in.gc.NativeView()
					handle := unsafe.Slice((*byte)(offHeapPtr(view.Handles+uintptr(ref>>1)*gc.NativeHandleStride)), gc.NativeHandleStride)
					header := unsafe.Slice((*byte)(offHeapPtr(view.Spaces[handle[gc.NativeHandleSpaceOffset]].Base+uintptr(binary.LittleEndian.Uint32(handle)))), gc.HeaderSize)
					old := binary.LittleEndian.Uint32(header[4:])
					binary.LittleEndian.PutUint32(header[4:], gc.PayloadOffset+4)
					defer binary.LittleEndian.PutUint32(header[4:], old)
				}
			}
			got, e := in.Invoke("run")
			if name == "success" {
				if e != nil || len(got) != 1 || got[0] != 0 {
					t.Fatalf("run=%v,%v", got, e)
				}
			} else {
				var trap *TrapError
				if !errors.As(e, &trap) {
					t.Fatalf("run trap=%v", e)
				}
				want := TrapCastFailure
				if name == "bounds" {
					want = TrapBuiltin
				}
				if trap.Code != want {
					t.Fatalf("trap=%v want%d", e, want)
				}
			}
			for g := 0; g < 8; g++ {
				if got := readGlobalObject(in.globalCells[g], in.c.Globals[g].Type); got != uint64(g+1) {
					t.Errorf("global%d=%#x want%d", g, got, g+1)
				}
			}
		})
	}
}
