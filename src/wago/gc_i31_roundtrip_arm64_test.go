//go:build (linux || darwin) && arm64 && !tinygo

package wago

import (
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func i31RoundTripModule(body []byte, params, results int) []byte {
	p, r := make([]wasm.ValType, params), make([]wasm.ValType, results)
	for i := range p {
		p[i] = wasm.I32
	}
	for i := range r {
		r[i] = wasm.I32
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(p, r))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestGCI31RoundTrip(t *testing.T) {
	for _, signed := range []bool{false, true} {
		get := byte(30)
		if signed {
			get = 29
		}
		for _, separated := range []bool{false, true} {
			body := []byte{0x20, 0, 0xfb, 28}
			if separated {
				body = append(body, 0x01) // nop must keep the ordinary path valid
			}
			body = append(body, 0xfb, get, 0x0b)
			c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), i31RoundTripModule(body, 1, 1))
			if err != nil {
				t.Fatal(err)
			}
			for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
				in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile}})
				if err != nil {
					t.Fatal(err)
				}
				for _, x := range []uint64{0, 1, 0x3fffffff, 0x40000000, 0x7fffffff, 0x80000000, 0xbfffffff, 0xc0000000, 0xffffffff, 0x12345678ffffffff} {
					want := uint32(x) & 0x7fffffff
					if signed {
						want = uint32(int32(uint32(x)<<1) >> 1)
					}
					got, err := in.Invoke("run", x)
					if err != nil || len(got) != 1 || got[0] != uint64(want) {
						t.Fatalf("signed=%v separated=%v profile=%v x=%x: %v,%v; want %x", signed, separated, profile, x, got, err, want)
					}
				}
				in.Close()
			}
			c.Close()
		}
	}
}

func TestGCI31RoundTripLivePrefixAndTrapOrder(t *testing.T) {
	// A deferred unsigned division precedes a second trapping producer. The
	// first trap must remain divide-by-zero, ahead of signed overflow.
	body := []byte{0x41, 1, 0x20, 0, 0x6e, 0x20, 1, 0x41, 0x7f, 0x6d, 0xfb, 28, 0xfb, 29, 0x0b}
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), i31RoundTripModule(body, 2, 2))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	in, err := Instantiate(c, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	for _, tc := range []struct {
		divisor, value uint64
		trap           TrapCode
	}{{0, 0x80000000, TrapDivZero}, {1, 0x80000000, TrapDivOverflow}} {
		_, err := in.Invoke("run", tc.divisor, tc.value)
		var trap *TrapError
		if !errors.As(err, &trap) || trap.Code != tc.trap {
			t.Fatalf("divisor %d value %x: %v; want %v", tc.divisor, tc.value, err, tc.trap)
		}
	}
	got, err := in.Invoke("run", 1, 5)
	if err != nil || len(got) != 2 || got[0] != 1 || uint32(got[1]) != 0xfffffffb {
		t.Fatalf("live prefix: %v,%v; want [1 -5]", got, err)
	}
}

func TestGCI31RoundTripMemoryTrapOrder(t *testing.T) {
	data := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}))),
		wasmtest.Section(3, wasmtest.Vec([]byte{0})),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x28, 2, 0, 0x20, 1, 0x41, 0x7f, 0x6d, 0xfb, 28, 0xfb, 30, 0x0b}))),
	)
	c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, profile := range []GCProfile{GCProfileThroughput, GCProfileTiny} {
		in, err := Instantiate(c, InstantiateOptions{GC: GCConfig{Profile: profile}})
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			address uint64
			trap    TrapCode
		}{{65536, TrapLinMemOutOfBounds}, {0, TrapDivOverflow}} {
			_, err := in.Invoke("run", tc.address, 0x80000000)
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != tc.trap {
				t.Fatalf("profile %v address %d: %v; want %v", profile, tc.address, err, tc.trap)
			}
		}
		in.Close()
	}
}

func TestGCI31RoundTripRegisterPressure(t *testing.T) {
	for _, get := range []byte{29, 30} {
		var body []byte
		for i := byte(0); i < 13; i++ {
			body = append(body, 0x20, i)
		}
		body = append(body, 0xfb, 28, 0xfb, get, 0x0b)
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), i31RoundTripModule(body, 13, 13))
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		args := make([]uint64, 13)
		for i := range args {
			args[i] = uint64(0xdeadbe00 + i)
		}
		got, err := in.Invoke("run", args...)
		if err != nil || len(got) != len(args) {
			t.Fatalf("get%d: %v,%v", get, got, err)
		}
		for i, value := range got {
			want := args[i]
			if i == 12 {
				if get == 29 {
					want = uint64(uint32(int32(uint32(want)<<1) >> 1))
				} else {
					want &= 0x7fffffff
				}
			}
			if value != want {
				t.Fatalf("get%d result%d: %x, want %x", get, i, value, want)
			}
		}
		in.Close()
		c.Close()
	}
}

func TestGCI31RoundTripLocalIdentity(t *testing.T) {
	for _, get := range []byte{29, 30} {
		body := []byte{0x20, 0, 0x20, 0, 0xfb, 28, 0xfb, get, 0x20, 0, 0x0b}
		c, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), i31RoundTripModule(body, 1, 3))
		if err != nil {
			t.Fatal(err)
		}
		in, err := Instantiate(c, InstantiateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range []uint64{0x40000000, 0x7fffffff, 0x80000000, 0xdeadbeef, 0xffffffff} {
			want := uint32(x) & 0x7fffffff
			if get == 29 {
				want = uint32(int32(uint32(x)<<1) >> 1)
			}
			got, err := in.Invoke("run", x)
			if err != nil || len(got) != 3 || got[0] != x || got[1] != uint64(want) || got[2] != x {
				t.Fatalf("get%d x%x: %v,%v; want [%x %x %x]", get, x, got, err, x, want, x)
			}
		}
		in.Close()
		c.Close()
	}
}
