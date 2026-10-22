//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"os"
	"reflect"
	"testing"
)

func vectorArrayAccessModule(t testing.TB) *Compiled {
	t.Helper()
	data, err := os.ReadFile("../../tests/fixtures/wasm/gc-array-vector-access.wasm")
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

func TestGCVectorArrayAccess(t *testing.T) {
	if !hostSupportsSIMD() {
		t.Skip("host SIMD unavailable")
	}
	c := vectorArrayAccessModule(t)
	for _, cfg := range []GCConfig{{VerifyAfterCollect: true}, {CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true}, {Profile: GCProfileTiny, TinyHeapBytes: 4096, VerifyAfterCollect: true}} {
		in, err := Instantiate(c, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer in.Close()
			if _, err := in.Invoke("get", 0); err == nil {
				t.Fatal("null vector array.get did not trap")
			}
			if _, err := in.Invoke("set", 0, 1, 2); err == nil {
				t.Fatal("null vector array.set did not trap")
			}
			if _, err := in.Invoke("init"); err != nil {
				t.Fatal(err)
			}
			for _, i := range []uint64{0, 63} {
				lo, hi := uint64(0x0123456789abcdef)+i, uint64(0xfedcba9876543210)^i
				if _, err := in.Invoke("set", i, lo, hi); err != nil {
					t.Fatal(err)
				}
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
				got, err := in.Invoke("get", i)
				if err != nil || !reflect.DeepEqual(got, []uint64{lo, hi}) {
					t.Fatalf("get(%d)=%x,%v", i, got, err)
				}
			}
			for _, i := range []uint64{64, 0xffffffff} {
				if _, err := in.Invoke("get", i); err == nil {
					t.Fatalf("get(%d) did not trap", i)
				}
				if _, err := in.Invoke("set", i, 1, 2); err == nil {
					t.Fatalf("set(%d) did not trap", i)
				}
			}
			if _, err := in.Invoke("init"); err != nil {
				t.Fatal(err)
			}
			if got, err := in.Invoke("read", 4096); err != nil || len(got) != 1 || got[0] != 7*4096 {
				t.Fatalf("read=%v,%v", got, err)
			}
			if got, err := in.Invoke("write", 4096); err != nil || len(got) != 1 || got[0] != 4095 {
				t.Fatalf("write=%v,%v", got, err)
			}
		}()
	}
}

func BenchmarkGCVectorArrayAccess(b *testing.B) {
	if !hostSupportsSIMD() {
		b.Skip("host SIMD unavailable")
	}
	c := vectorArrayAccessModule(b)
	for _, name := range []string{"read", "write"} {
		b.Run(name, func(b *testing.B) {
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			if _, err := in.Invoke("init"); err != nil {
				b.Fatal(err)
			}
			const n = 8192
			want := uint64(7 * n)
			if name == "write" {
				want = n - 1
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := in.Invoke(name, n)
				if err != nil || len(got) != 1 || got[0] != want {
					b.Fatalf("%s=%v,%v", name, got, err)
				}
			}
		})
	}
}
