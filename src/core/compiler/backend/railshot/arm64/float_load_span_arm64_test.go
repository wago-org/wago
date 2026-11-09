//go:build (linux || darwin) && arm64

package arm64

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestFloatLoadSpanDeclaredHeadroom(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limits wasm.Limits
		shared bool
		delta  uint32
		want   bool
	}{
		{"small", wasm.Limits{Min: 1, Max: 36, HasMax: true}, false, 64, true},
		{"unbounded", wasm.Limits{Min: 1}, false, 64, false},
		{"full32", wasm.Limits{Min: 1, Max: 65536, HasMax: true}, false, 64, false},
		{"memory64", wasm.Limits{Min: 1, Max: 36, HasMax: true, Addr64: true}, false, 64, false},
		{"shared", wasm.Limits{Min: 1, Max: 36, HasMax: true}, true, 64, false},
		{"largeDelta", wasm.Limits{Min: 1, Max: 36, HasMax: true}, false, 1 << 21, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fn{m: &wasm.Module{Memories: []wasm.MemType{{Limits: tc.limits, Shared: tc.shared}}}}
			if got := f.addressOffsetCannotWrap(tc.delta); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			f.localBase = 1
			if f.addressOffsetCannotWrap(tc.delta) {
				t.Fatal("accepted inlined local namespace")
			}
		})
	}
}

func TestFloatLoadSpanProbeBarriers(t *testing.T) {
	old := floatLoadSpanEnabled
	floatLoadSpanEnabled = true
	defer func() { floatLoadSpanEnabled = old }()
	f := fn{m: &wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Min: 1, Max: 36, HasMax: true}}}}}
	// Original load is already consumed. Its float result can pass through pure
	// arithmetic, then another load uses the same base plus a wrapping constant.
	prefix := []byte{0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0xa0}
	next := []byte{0x20, 0, 0x41, 0xc0, 0, 0x6a, 0x2b, 0, 0}
	for _, tc := range []struct {
		name    string
		barrier []byte
		want    int32
	}{
		{"pure", nil, 72}, {"store", []byte{0x39, 0, 0}, 8},
		{"call", []byte{0x10, 0}, 8}, {"trunc", []byte{0xaa}, 8},
		{"division", []byte{0x6d}, 8}, {"branch", []byte{0x0d, 0}, 8},
		{"sourceWrite", []byte{0x21, 0}, 8}, {"grow", []byte{0x40, 0}, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := append(append(append([]byte{}, prefix...), tc.barrier...), next...)
			r := wasm.NewReader(b)
			if got := f.straightLineLoadExtent(r, 1, 0, 8); got != tc.want {
				t.Fatalf("extent %d want %d", got, tc.want)
			}
			if r.Offset() != 0 {
				t.Fatal("probe changed original reader")
			}
		})
	}
	// Extending a span must not turn its new bounded window into the legacy long scan.
	b := append([]byte{}, prefix...)
	for i := 0; i < 130; i++ {
		b = append(b, 0x01)
	}
	b = append(b, next...)
	if got := f.straightLineLoadExtent(wasm.NewReader(b), 1, 0, 8); got != 8 {
		t.Fatalf("budget escape %d", got)
	}
}

func TestFloatLoadSpanMemoryAndPartialTraps(t *testing.T) {
	old := floatLoadSpanEnabled
	defer func() { floatLoadSpanEnabled = old }()
	initial := make([]byte, 65536)
	for i := 0; i < len(initial); i += 8 {
		binary.LittleEndian.PutUint64(initial[i:], math.Float64bits(float64(i)/8+0.25))
	}
	for _, storeBetween := range []bool{false, true} {
		// A preceding store must survive any subsequent OOB trap. A store between
		// loads must prevent widening the first check across that observable effect.
		body := []byte{0, 0x41, 0, 0x41, 17, 0x36, 0, 0, 0x20, 0, 0x2b, 0, 0}
		if storeBetween {
			body = append(body, 0x41, 4, 0x41, 23, 0x36, 0, 0)
		}
		body = append(body, 0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0xa0, 0x20, 0, 0x41, 0xc0, 0, 0x6a, 0x2b, 0, 0, 0xa0, 0x1a, 0x41, 8, 0x41, 31, 0x36, 0, 0, 0x41, 0, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
		m.Memories[0].Limits.Max = 36
		m.Memories[0].Limits.HasMax = true
		for _, base := range []uint64{16, 65504, 65528, 65536, 0xffffffc0, 0x8877665500000010} {
			for _, compact := range []bool{false, true} {
				want := append([]byte{}, initial...)
				binary.LittleEndian.PutUint32(want, 17)
				trap := uint64(uint32(base))+8 > 65536
				if !trap && storeBetween {
					binary.LittleEndian.PutUint32(want[4:], 23)
				}
				trap = trap || uint64(uint32(base)+64)+8 > 65536
				if !trap {
					binary.LittleEndian.PutUint32(want[8:], 31)
				}
				for _, on := range []bool{false, true} {
					floatLoadSpanEnabled = on
					got, err := runLinearSpanMemory(t, m, CompileOptions{CompactNative: compact, Optimizations: map[string]bool{"memory-min-bounds": false}}, initial, base)
					if (err != nil) != trap || !bytes.Equal(got, want) {
						t.Fatalf("between%v base%x compact%v on%v trap%v err%v equal%v", storeBetween, base, compact, on, trap, err, bytes.Equal(got, want))
					}
				}
			}
		}
	}
	// An unchecked original base may wrap into a valid derived address. It must
	// neither inherit nor establish a false certificate for that original base.
	body := []byte{0, 0x20, 0, 0x41, 0xc0, 0, 0x6a, 0x2b, 0, 0, 0x1a, 0x41, 0, 0x41, 29, 0x36, 0, 0, 0x20, 0, 0x2b, 0, 0, 0x1a, 0x41, 0, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	m.Memories[0].Limits.Max = 36
	m.Memories[0].Limits.HasMax = true
	for _, on := range []bool{false, true} {
		floatLoadSpanEnabled = on
		want := append([]byte{}, initial...)
		binary.LittleEndian.PutUint32(want, 29)
		got, err := runLinearSpanMemory(t, m, CompileOptions{}, initial, 0xffffffc0)
		if err == nil || !bytes.Equal(got, want) {
			t.Fatalf("wrapped original base on%v err%v equal%v", on, err, bytes.Equal(got, want))
		}
	}
}

func TestFloatLoadSpanEmitsSharedProof(t *testing.T) {
	old := floatLoadSpanEnabled
	defer func() { floatLoadSpanEnabled = old }()
	body := []byte{0, 0x20, 0, 0x2b, 0, 0, 0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0xa0, 0x20, 0, 0x41, 0xc0, 0, 0x6a, 0x2b, 0, 0, 0xa0, 0x0b}
	m := modMem(t, 1, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.F64}, body)
	m.Memories[0].Limits.Max = 36
	m.Memories[0].Limits.HasMax = true
	images := make([][]byte, 2)
	for i, on := range []bool{false, true} {
		floatLoadSpanEnabled = on
		var stats ModuleStats
		opts := CompileOptions{Optimizations: map[string]bool{"memory-min-bounds": false}}
		if diagnosticsEnabled {
			opts.Stats = &stats
		}
		cm, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatal(err)
		}
		images[i] = append([]byte{}, cm.Code...)
		cm.CodeImage.Close()
		if diagnosticsEnabled {
			t.Logf("on%v checks%d peepholes%v", on, stats.Funcs[0].BoundsChecks, stats.Funcs[0].Peephole)
			if on && stats.Funcs[0].BoundsChecks != 1 {
				t.Fatal("shared span did not remove the second check")
			}
		}
	}
	if bytes.Equal(images[0], images[1]) {
		t.Fatal("floating span did not change native checks")
	}
}
