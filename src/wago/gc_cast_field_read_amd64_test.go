//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/wago-org/wago/tests/support/wasmtest"
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

func openCastFieldReadModule() []byte {
	set := func(typeIndex byte) []byte {
		return []byte{0x20, 0, 0xfb, 0, typeIndex, 0x24, 0, 0x0b}
	}
	read := func(cast byte) []byte {
		return []byte{0x23, 0, 0xfb, cast, 0, 0xfb, 2, 0, 0, 0x0b}
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			[]byte{0x50, 0, 0x5f, 1, 0x7f, 0},
			[]byte{0x50, 1, 0, 0x5f, 1, 0x7f, 0},
			[]byte{0x4f, 0, 0x5f, 1, 0x7f, 0},
			[]byte{0x60, 1, 0x7f, 0},
			[]byte{0x60, 0, 1, 0x7f},
			[]byte{0x60, 0, 0},
		)),
		wasmtest.Section(3, wasmtest.Vec([]byte{3}, []byte{3}, []byte{3}, []byte{5}, []byte{4}, []byte{4})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x6e, 1, 0xd0, 0x6e, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("set-exact", 0, 0),
			wasmtest.ExportEntry("set-subtype", 0, 1),
			wasmtest.ExportEntry("set-other", 0, 2),
			wasmtest.ExportEntry("clear", 0, 3),
			wasmtest.ExportEntry("read", 0, 4),
			wasmtest.ExportEntry("read-nullable", 0, 5),
		)),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code(set(0)),
			wasmtest.Code(set(1)),
			wasmtest.Code(set(2)),
			wasmtest.Code([]byte{0xd0, 0x6e, 0x24, 0, 0x0b}),
			wasmtest.Code(read(22)),
			wasmtest.Code(read(23)),
		)),
	)
}

func TestGCOpenCastFieldRead(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), openCastFieldReadModule())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	configs := []GCConfig{
		{VerifyAfterCollect: true},
		{CollectEveryAlloc: true, StressNurseryBytes: 128, VerifyAfterCollect: true},
		{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, VerifyAfterCollect: true},
	}
	for _, cfg := range configs {
		in, err := Instantiate(c, InstantiateOptions{GC: cfg})
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer in.Close()
			for _, setup := range []string{"set-exact", "set-subtype"} {
				if _, err := in.Invoke(setup, 73); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"read", "read-nullable"} {
					got, err := in.Invoke(name)
					if err != nil || !reflect.DeepEqual(got, []uint64{73}) {
						t.Fatalf("profile=%d %s after %s=%v,%v; want73", cfg.Profile, name, setup, got, err)
					}
				}
				if err := in.CollectGC(); err != nil {
					t.Fatal(err)
				}
				got, err := in.Invoke("read")
				if err != nil || !reflect.DeepEqual(got, []uint64{73}) {
					t.Fatalf("profile=%d read after collection/%s=%v,%v; want73", cfg.Profile, setup, got, err)
				}
			}
			if _, err := in.Invoke("set-other", 91); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"read", "read-nullable"} {
				_, err := in.Invoke(name)
				var trap *TrapError
				if !errors.As(err, &trap) || trap.Code != TrapCastFailure {
					t.Fatalf("profile=%d %s unrelated=%v; want cast trap", cfg.Profile, name, err)
				}
			}
			if _, err := in.Invoke("clear"); err != nil {
				t.Fatal(err)
			}
			for name, code := range map[string]uint32{"read": uint32(TrapCastFailure), "read-nullable": uint32(TrapNullReference)} {
				_, err := in.Invoke(name)
				var trap *TrapError
				if !errors.As(err, &trap) || uint32(trap.Code) != code {
					t.Fatalf("profile=%d %s null=%v; want trap%d", cfg.Profile, name, err, code)
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
