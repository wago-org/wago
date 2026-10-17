//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func gcResolverArithmeticModule(sites int, reassign bool) []byte {
	body := []byte{2, 1, 0x63, 0, 1, 0x7f, 0x23, 0, 0x21, 1, 0x03, 0x40}
	if reassign {
		body = []byte{3, 1, 0x63, 0, 1, 0x7f, 1, 0x63, 0, 0x23, 1, 0x21, 3, 0x23, 0, 0x21, 1, 0x03, 0x40}
	}
	for i := 0; i < sites; i++ {
		body = append(body, 0x20, 2, 0x20, 1, 0xfb, 2, 0, 0, 0x6a, 0x21, 2)
		if reassign && i == 0 {
			body = append(body, 0x20, 3, 0x21, 1)
		}
	}
	body = append(body, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
	init := wasmtest.Code([]byte{0x41, 7, 0xfb, 0, 0, 0x24, 0, 0x41, 11, 0xfb, 0, 0, 0x24, 1, 0x0b})
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})),
		wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b}, []byte{0x63, 0, 1, 0xd0, 0, 0x0b})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(init, append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestGCResolverAcrossArithmetic(t *testing.T) {
	for _, reassign := range []bool{false, true} {
		for _, sites := range []int{1, 4, 16} {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcResolverArithmeticModule(sites, reassign))
			if err != nil {
				t.Fatal(err)
			}
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = in.Invoke("init"); err != nil {
				t.Fatal(err)
			}
			got, err := in.Invoke("run", 100)
			want := uint64(100 * sites * 7)
			if reassign {
				want = uint64(100*sites*11 - 4)
			}
			if err != nil || len(got) != 1 || got[0] != want {
				t.Fatalf("sites%d reassign%v: %v,%v want%d", sites, reassign, got, err, want)
			}
			in.Close()
			c.Close()
		}
	}
}

func BenchmarkGCResolverAcrossArithmetic(b *testing.B) {
	for _, sites := range []int{1, 4, 16} {
		b.Run(fmt.Sprint(sites), func(b *testing.B) {
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcResolverArithmeticModule(sites, false))
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			if _, err = in.Invoke("init"); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ReportMetric(float64(len(c.code)), "code-B")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := in.Invoke("run", 1024)
				if err != nil || len(got) != 1 || got[0] != uint64(1024*sites*7) {
					b.Fatalf("%v,%v", got, err)
				}
			}
		})
	}
}

func BenchmarkGCResolverAcrossArithmeticCompile(b *testing.B) {
	for _, sites := range []int{1, 4, 16, 256} {
		b.Run(fmt.Sprint(sites), func(b *testing.B) {
			binary := gcResolverArithmeticModule(sites, false)
			cfg := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				c, err := Compile(cfg, binary)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(c.code)), "code-B")
				c.Close()
			}
		})
	}
}

// Keep the resolved address live while a deferred integer tree needs registers.
// A canonical stack rebuild must not make that address register allocatable.
func TestGCResolverAcrossArithmeticPressure(t *testing.T) {
	const count = 64
	params := make([]wasm.ValType, count)
	args := make([]uint64, count)
	for i := range params {
		params[i] = wasm.I64
		args[i] = uint64(i)*0x102030405 + 17
	}
	var tree func(int, int) ([]byte, uint64)
	tree = func(lo, hi int) ([]byte, uint64) {
		if hi-lo == 1 {
			return []byte{0x20, byte(lo)}, args[lo]
		}
		mid := (lo + hi) / 2
		a, x := tree(lo, mid)
		b, y := tree(mid, hi)
		a = append(a, b...)
		switch (hi - lo) % 3 {
		case 0:
			return append(a, 0x85), x ^ y
		case 1:
			return append(a, 0x7e), x * y
		default:
			return append(a, 0x7c), x + y
		}
	}
	body := []byte{1, 1, 0x63, 0, 0x23, 0, 0x21, count}
	for i := 0; i < 2; i++ {
		body = append(body, 0x20, count, 0xfb, 2, 0, 0, 0x1a)
	}
	expr, want := tree(0, count)
	body = append(body, expr...)
	body = append(body, 0x20, count, 0xfb, 2, 0, 0, 0x0b)
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType(nil, nil), wasmtest.FuncType(params, []wasm.ValType{wasm.I64, wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})), wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 7, 0xfb, 0, 0, 0x24, 0, 0x0b}), append(wasmtest.ULEB(uint32(len(body))), body...))))
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err = in.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	got, err := in.Invoke("run", args...)
	if err != nil || len(got) != 2 || got[0] != want || got[1] != 7 {
		t.Fatalf("got%v,%v want[%d 7]", got, err, want)
	}
}

func TestGCResolverAcrossCollectingCall(t *testing.T) {
	body := []byte{2, 1, 0x63, 0, 1, 0x7f, 0x41, 7, 0xfb, 0, 0, 0x21, 1, 0x20, 1, 0xfb, 2, 0, 0, 0x1a, 0x20, 0, 0x41, 3, 0x6a, 0x21, 2, 0x10, 1, 0x20, 1, 0xfb, 2, 0, 0, 0x20, 2, 0x6a, 0x0b}
	data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType(nil, nil), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))), wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{1}, []byte{2})), wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b}, []byte{0x63, 0, 1, 0xd0, 0, 0x0b})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 2))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x0b}), wasmtest.Code([]byte{0xfb, 1, 0, 0x24, 1, 0x0b}), append(wasmtest.ULEB(uint32(len(body))), body...))))
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// Each object occupies 24 bytes: the second allocation must evacuate the
	// first from a 32-byte nursery. CollectEveryAlloc deliberately mixes minor
	// and full collections in debug builds and cannot guarantee movement.
	in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{StressNurseryBytes: 32, SurvivorBytes: 32, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err = in.Invoke("init"); err != nil {
		t.Fatal(err)
	}
	before := in.gc.Stats()
	got, err := in.Invoke("run", 5)
	if err != nil || len(got) != 1 || got[0] != 15 {
		t.Fatalf("%v,%v", got, err)
	}
	after := in.gc.Stats()
	if after.MinorCollections <= before.MinorCollections || after.YoungBytesCopied <= before.YoungBytesCopied {
		t.Fatalf("call did not collect and copy: before=%+v after=%+v", before, after)
	}
}

func TestGCResolverSourceAssignment(t *testing.T) {
	for _, tee := range []bool{false, true} {
		for _, borrowed := range []bool{false, true} {
			body := []byte{1, 2, 0x63, 0, 0x23, 0, 0x21, 0, 0x23, 1, 0x21, 1, 0x20, 0, 0xfb, 2, 0, 0, 0x1a}
			if borrowed {
				body = append(body, 0x20, 0)
			}
			body = append(body, 0x20, 1)
			if tee {
				body = append(body, 0x22, 0, 0x1a)
			} else {
				body = append(body, 0x21, 0)
			}
			results := []wasm.ValType{wasm.I32}
			if borrowed {
				body = append(body, 0xfb, 2, 0, 0)
				results = append(results, wasm.I32)
			}
			body = append(body, 0x20, 0, 0xfb, 2, 0, 0, 0x0b)
			init := wasmtest.Code([]byte{0x41, 7, 0xfb, 0, 0, 0x24, 0, 0x41, 11, 0xfb, 0, 0, 0x24, 1, 0x0b})
			data := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec([]byte{0x5f, 1, 0x7f, 1}, wasmtest.FuncType(nil, nil), wasmtest.FuncType(nil, results))), wasmtest.Section(3, wasmtest.Vec([]byte{1}, []byte{2})), wasmtest.Section(6, wasmtest.Vec([]byte{0x63, 0, 1, 0xd0, 0, 0x0b}, []byte{0x63, 0, 1, 0xd0, 0, 0x0b})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("init", 0, 0), wasmtest.ExportEntry("run", 0, 1))), wasmtest.Section(10, wasmtest.Vec(init, append(wasmtest.ULEB(uint32(len(body))), body...))))
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
			if err != nil {
				t.Fatal(err)
			}
			in, err := Instantiate(c, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = in.Invoke("init"); err != nil {
				t.Fatal(err)
			}
			got, err := in.Invoke("run")
			if err != nil || len(got) != len(results) || got[len(got)-1] != 11 || borrowed && got[0] != 7 {
				t.Fatalf("tee%v borrowed%v: %v,%v", tee, borrowed, got, err)
			}
			in.Close()
			c.Close()
		}
	}
}
