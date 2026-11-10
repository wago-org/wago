//go:build ((linux && (amd64 || arm64)) || (darwin && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
	"math/rand"
	"testing"
)

func gcI31ReductionModule(prelude bool, get, step, compare byte) []byte {
	body := []byte{1, 2, 0x7f}
	if prelude {
		body = append(body, 1)
	}
	body = append(body, 0x03, 0x40, 0x20, 2, 0x20, 1, 0xfb, 28, 0xfb, get, 0x6a, 0x21, 2,
		0x20, 1, 0x41, step, 0x6a, 0x21, 1, 0x20, 1, 0x20, 0, compare, 0x0d, 0, 0x0b, 0x20, 2, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(append(wasmtest.ULEB(uint32(len(body))), body...))))
}

func TestGCI31ReductionFullDomain(t *testing.T) {
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcI31ReductionModule(false, 30, 1, 0x49))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !c.directPreparedBoundedAt(0) {
		t.Fatal("induction reduction was not admitted as bounded")
	}
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	fn, err := in.WasmFunc("run")
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcI31ReductionModule(true, 30, 1, 0x49))
	if err != nil {
		t.Fatal(err)
	}
	defer ordinary.Close()
	slow, err := Instantiate(ordinary, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer slow.Close()
	for n := uint64(0); n <= 1024; n++ {
		got, err := fn.Invoke(n)
		want, e := slow.Invoke("run", n)
		if err != nil || e != nil || len(got) != 1 || len(want) != 1 || got[0] != want[0] {
			t.Fatalf("n=%d got=%v want=%v errors=%v/%v", n, got, want, err, e)
		}
	}
	values := []uint64{0, 1, 2, 4096, 0x3fffffff, 0x40000000, 0x7ffffffe, 0x7fffffff, 0x80000000, 0x80000001, 0xfffffffe, 0xffffffff, 0x12345678ffffffff}
	random := rand.New(rand.NewSource(966))
	for i := 0; i < 1000; i++ {
		values = append(values, uint64(random.Uint32()))
	}
	for _, input := range values {
		n := uint64(uint32(input))
		const period = uint64(1) << 31
		q, r := n/period, n%period
		// Independent oracle: complete periods plus the remaining prefix.
		want := uint64(uint32(q*(period*(period-1)/2) + r*(r-1)/2))
		got, err := fn.Invoke(input)
		if err != nil || len(got) != 1 || got[0] != want {
			t.Fatalf("input=%x got=%v err=%v want=%x", input, got, err, want)
		}
	}
}

func TestGCI31ReductionRejectsDifferentLoops(t *testing.T) {
	for _, tc := range []struct {
		prelude            bool
		get, step, compare byte
	}{
		{true, 30, 1, 0x49}, {false, 29, 1, 0x49}, {false, 30, 2, 0x49}, {false, 30, 1, 0x48},
	} {
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), gcI31ReductionModule(tc.prelude, tc.get, tc.step, tc.compare))
		if err != nil {
			t.Fatal(err)
		}
		if c.directPreparedBoundedAt(0) {
			t.Fatalf("unproven loop admitted: %+v", tc)
		}
		c.Close()
	}
}
