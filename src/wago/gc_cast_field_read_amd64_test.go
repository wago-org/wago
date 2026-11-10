//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"errors"
	"os"
	"reflect"
	"testing"
)

func castFieldReadModule(t testing.TB) *Compiled {
	t.Helper()
	requireCompleteCore3Backend(t)
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-cast-field-read.wasm")
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

func TestGCFinalCastFieldRead(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	c := castFieldReadModule(t)
	wants := map[string][]uint64{"i32": {7}, "nullable-i32": {7}, "i64": {0x0102030405060708}, "f32": {0x3fa00000}, "f64": {0xc01a000000000000}, "i8-s": {0xfffffff9}, "i8-u": {249}, "i16-s": {0xfffffb2e}, "i16-u": {64302}, "v128": {11, 22}}
	for _, cfg := range []GCConfig{{VerifyAfterCollect: true}, {CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyHeapBytes: 4096, TinyBlockBytes: 16, VerifyAfterCollect: true}} {
		in, err := Instantiate(c, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer in.Close()
			if _, err := in.Invoke("init"); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				for name, want := range wants {
					got, err := in.Invoke(name)
					if err != nil || !reflect.DeepEqual(got, want) {
						t.Fatalf("%s=%x,%v; want %x", name, got, err, want)
					}
				}
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
			}
			for _, setup := range []string{"clear", "other"} {
				if _, err := in.Invoke(setup); err != nil {
					t.Fatal(err)
				}
				for name := range wants {
					_, err := in.Invoke(name)
					var trap *TrapError
					want := TrapCastFailure
					if setup == "clear" && name == "nullable-i32" {
						want = TrapNullReference
					}
					if !errors.As(err, &trap) || trap.Code != want {
						t.Fatalf("%s after %s=%v; want trap %v", name, setup, err, want)
					}
				}
			}
		}()
	}
}

func BenchmarkGCFinalCastFieldRead(b *testing.B) {
	if !hostSupportsSIMD() {
		b.Skip("host SIMD unavailable")
	}
	c := castFieldReadModule(b)
	for _, tc := range []struct {
		name string
		per  uint64
	}{{"loop-i32", 7}, {"loop-v128", 22}} {
		b.Run(tc.name, func(b *testing.B) {
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			if _, err := in.Invoke("init"); err != nil {
				b.Fatal(err)
			}
			const n = 8192
			invoke := func() {
				got, err := in.Invoke(tc.name, n)
				if err != nil || len(got) != 1 || got[0] != tc.per*n {
					b.Fatalf("%s=%v,%v", tc.name, got, err)
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
