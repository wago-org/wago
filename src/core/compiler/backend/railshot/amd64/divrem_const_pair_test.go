//go:build linux && amd64

package amd64

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func constDivRemPairBody(d uint32, consumer byte, alias bool) []byte {
	body := []byte{0, 0x20, 0, 0x41}
	body = append(body, wasmtest.SLEB32(int32(d))...)
	body = append(body, 0x6e, 0x20, 0, 0x41)
	body = append(body, wasmtest.SLEB32(int32(d))...)
	body = append(body, 0x70, consumer)
	if alias {
		body = append(body, 0x21, 0, 0x20, 0)
	}
	return append(body, 0x0b)
}

func TestDivRemConstPairValues(t *testing.T) {
	saved, scalar := divRemConstPairEnabled, sharedScalarEnabled
	sharedScalarEnabled = false
	defer func() { divRemConstPairEnabled, sharedScalarEnabled = saved, scalar }()
	values := []uint32{0, 1, 6, 7, 8, 0x7fffffff, 0x80000000, 0xfffffffe, 0xffffffff}
	rng := rand.New(rand.NewSource(19))
	for range 128 {
		values = append(values, rng.Uint32())
	}
	for _, d := range []uint32{1, 2, 3, 7, 8, 10, 97, 65535, 0x80000000, 0xfffffffb, 0xffffffff} {
		for _, consumer := range []byte{0x6a, 0x6b, 0x73} { // add, sub, xor need both results
			for _, alias := range []bool{false, true} {
				t.Run(fmt.Sprintf("d=%x/op=%x/alias=%v", d, consumer, alias), func(t *testing.T) {
					m := mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32}, constDivRemPairBody(d, consumer, alias))
					for _, on := range []bool{false, true} {
						divRemConstPairEnabled = on
						var stats ModuleStats
						cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
						if err != nil {
							t.Fatal(err)
						}
						if cm.CodeImage != nil {
							defer cm.CodeImage.Close()
						}
						if diagnosticsEnabled && d&(d-1) != 0 && (stats.Funcs[0].Peephole["divrem-const-pair"] != 0) != on {
							t.Fatalf("on=%v hits=%v", on, stats.Funcs[0].Peephole)
						}
						for _, x := range values {
							q, r := x/d, x%d
							want := q + r
							if consumer == 0x6b {
								want = q - r
							} else if consumer == 0x73 {
								want = q ^ r
							}
							if got := runCompiledAmd64u(t, cm, uint64(x)); got != uint64(want) {
								t.Fatalf("on=%v x=%x got=%x want=%x", on, x, got, want)
							}
						}
					}
				})
			}
		}
	}
}

func TestDivRemConstPairPressure(t *testing.T) {
	saved, scalar := divRemConstPairEnabled, sharedScalarEnabled
	divRemConstPairEnabled, sharedScalarEnabled = true, false
	defer func() { divRemConstPairEnabled, sharedScalarEnabled = saved, scalar }()
	const depth = 24
	body := []byte{0}
	for level := range depth {
		body = append(body, 0x20, 0, 0x41, byte(level+1), 0x73)
	}
	pair := constDivRemPairBody(7, 0x6a, false)
	body = append(body, pair[1:len(pair)-1]...)
	for range depth {
		body = append(body, 0x73)
	}
	body = append(body, 0x0b)
	m := mod1(t, []wasm.ValType{i32}, []wasm.ValType{i32}, body)
	var stats ModuleStats
	cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
	if err != nil {
		t.Fatal(err)
	}
	if cm.CodeImage != nil {
		defer cm.CodeImage.Close()
	}
	if diagnosticsEnabled && stats.Funcs[0].Peephole["divrem-const-pair"] != 1 {
		t.Fatalf("hits=%v", stats.Funcs[0].Peephole)
	}
	for _, x := range []uint32{0, 7, 0x80000000, 0xffffffff} {
		want := x/7 + x%7
		for level := range depth {
			want ^= x ^ uint32(level+1)
		}
		if got := runCompiledAmd64u(t, cm, uint64(x)); got != uint64(want) {
			t.Fatalf("x=%x got=%x want=%x", x, got, want)
		}
	}
}

func TestDivRemConstPairLiveFixedRegisterValues(t *testing.T) {
	saved, scalar := divRemConstPairEnabled, sharedScalarEnabled
	defer func() { divRemConstPairEnabled, sharedScalarEnabled = saved, scalar }()
	for _, shared := range []bool{false, true} {
		sharedScalarEnabled = shared
		for _, op := range []byte{0x6d, 0x6f} { // div_s returns RAX; rem_s returns RDX
			for _, depth := range []int{1, 13} {
				t.Run(fmt.Sprintf("shared=%v/op=%x/depth=%d", shared, op, depth), func(t *testing.T) {
					// Each dynamic divide must execute. Its raw RAX/RDX result remains
					// live below the constant pair until the final XOR reductions.
					body := []byte{0}
					for level := range depth {
						body = append(body, 0x20, 0, 0x41, byte(level+1), 0x6a, 0x20, 1, op)
					}
					pair := constDivRemPairBody(7, 0x6a, false)
					pair[2], pair[7] = 2, 2 // both dividends are parameter2
					body = append(body, pair[1:len(pair)-1]...)
					for range depth {
						body = append(body, 0x73)
					}
					body = append(body, 0x0b)
					m := mod1(t, []wasm.ValType{i32, i32, i32}, []wasm.ValType{i32}, body)
					for _, on := range []bool{false, true} {
						divRemConstPairEnabled = on
						var stats ModuleStats
						cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
						if err != nil {
							t.Fatal(err)
						}
						if cm.CodeImage != nil {
							defer cm.CodeImage.Close()
						}
						if diagnosticsEnabled {
							st := stats.Funcs[0]
							minimumSpills := 1
							if depth > 1 {
								minimumSpills = 2
							}
							if !st.SharedScalar && ((st.Peephole["divrem-const-pair"] != 0) != on || st.Spills < minimumSpills) {
								t.Fatalf("on=%v pair=%d spills=%d want>=%d", on, st.Peephole["divrem-const-pair"], st.Spills, minimumSpills)
							}
							t.Logf("on=%v shared-body=%v pair=%d spills=%d reloads=%d", on, st.SharedScalar, st.Peephole["divrem-const-pair"], st.Spills, st.Reloads)
						}
						for _, args := range [][3]uint32{{20, 3, 100}, {0xffffffec, 3, 0xffffffe0}, {0x80000000, 7, 0xffffffff}} {
							want := args[2]/7 + args[2]%7
							for level := range depth {
								x := int32(args[0] + uint32(level+1))
								value := x / int32(args[1])
								if op == 0x6f {
									value = x % int32(args[1])
								}
								want ^= uint32(value)
							}
							if got := runCompiledAmd64u(t, cm, uint64(args[0]), uint64(args[1]), uint64(args[2])); got != uint64(want) {
								t.Fatalf("on=%v args=%x got=%x want=%x", on, args, got, want)
							}
						}
					}
				})
			}
		}
	}
}

func TestDivRemConstPairFallbacks(t *testing.T) {
	saved, scalar := divRemConstPairEnabled, sharedScalarEnabled
	divRemConstPairEnabled, sharedScalarEnabled = true, false
	defer func() { divRemConstPairEnabled, sharedScalarEnabled = saved, scalar }()
	signedInput := int32(-2147483648)
	signedResult := uint64(uint32(signedInput/7 + signedInput%7))
	for _, tc := range []struct {
		name   string
		params []wasm.ValType
		result wasm.ValType
		body   []byte
		args   []uint64
		want   uint64
	}{
		{"signed", []wasm.ValType{i32}, i32, []byte{0, 0x20, 0, 0x41, 7, 0x6d, 0x20, 0, 0x41, 7, 0x6f, 0x6a, 0x0b}, []uint64{0x80000000}, signedResult},
		{"i64", []wasm.ValType{i64}, i64, []byte{0, 0x20, 0, 0x42, 7, 0x80, 0x20, 0, 0x42, 7, 0x82, 0x7c, 0x0b}, []uint64{0xffffffffffffffff}, ^uint64(0)/7 + ^uint64(0)%7},
		{"different-local", []wasm.ValType{i32, i32}, i32, []byte{0, 0x20, 0, 0x41, 7, 0x6e, 0x20, 1, 0x41, 7, 0x70, 0x6a, 0x0b}, []uint64{100, 12}, 19},
		{"different-divisor", []wasm.ValType{i32}, i32, []byte{0, 0x20, 0, 0x41, 7, 0x6e, 0x20, 0, 0x41, 3, 0x70, 0x6a, 0x0b}, []uint64{100}, 15},
		// The local mutation forces the earlier read to retain its old value.
		{"local-mutation", []wasm.ValType{i32}, i32, []byte{0, 0x20, 0, 0x41, 7, 0x6e, 0x41, 12, 0x21, 0, 0x20, 0, 0x41, 7, 0x70, 0x6a, 0x0b}, []uint64{100}, 19},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := mod1(t, tc.params, []wasm.ValType{tc.result}, tc.body)
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if diagnosticsEnabled && stats.Funcs[0].Peephole["divrem-const-pair"] != 0 {
				t.Fatalf("unexpected pair: %v", stats.Funcs[0].Peephole)
			}
			if got := runCompiledAmd64u(t, cm, tc.args...); got != tc.want {
				t.Fatalf("got=%x want=%x", got, tc.want)
			}
		})
	}
}

func TestDivRemConstPairTrapAndGuardFallback(t *testing.T) {
	saved, scalar := divRemConstPairEnabled, sharedScalarEnabled
	sharedScalarEnabled = false
	defer func() { divRemConstPairEnabled, sharedScalarEnabled = saved, scalar }()
	for _, on := range []bool{false, true} {
		divRemConstPairEnabled = on
		for _, guard := range []bool{false, true} {
			m := modMem(t, 1, []wasm.ValType{i32}, []wasm.ValType{i32}, constDivRemPairBody(7, 0x6a, false))
			var stats ModuleStats
			cm, err := CompileModuleWith(m, CompileOptions{ElideBoundsChecks: guard, Stats: optionalTestStats(&stats)})
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				cm.CodeImage.Close()
			}
			if diagnosticsEnabled && (stats.Funcs[0].Peephole["divrem-const-pair"] != 0) != (on && !guard) {
				t.Fatalf("on=%v guard=%v hits=%v", on, guard, stats.Funcs[0].Peephole)
			}
			if diagnosticsEnabled {
				t.Logf("on=%v guard=%v pair=%d", on, guard, stats.Funcs[0].Peephole["divrem-const-pair"])
			}
			if got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{ElideBoundsChecks: guard}, nil, 0xffffffff); err != nil || got != uint64(uint32(0xffffffff)/7+uint32(0xffffffff)%7) {
				t.Fatalf("on=%v guard=%v got=%x err=%v", on, guard, got, err)
			}
			for _, tc := range []struct {
				d       uint32
				earlier bool
			}{{0, false}, {0, true}, {7, true}} {
				body := constDivRemPairBody(tc.d, 0x6a, false)
				want := runtime.TrapDivZero
				if tc.earlier {
					prefix := []byte{0, 0x41}
					prefix = append(prefix, wasmtest.SLEB32(-2147483648)...)
					prefix = append(prefix, 0x41, 0x7f, 0x6d)
					body = append(prefix, body[1:len(body)-1]...)
					body = append(body, 0x6a, 0x0b)
					want = runtime.TrapDivOverflow
				}
				m = modMem(t, 1, []wasm.ValType{i32}, []wasm.ValType{i32}, body)
				_, _, err := runMemAmd64WithOptions(t, m, CompileOptions{ElideBoundsChecks: guard}, nil, 7)
				var trap *runtime.TrapError
				if !errors.As(err, &trap) || trap.Code != want {
					t.Fatalf("on=%v guard=%v d=%v earlier=%v trap=%v want=%v", on, guard, tc.d, tc.earlier, err, want)
				}
			}
		}
	}
}

func TestDivRemConstPairExactFeature(t *testing.T) {
	saved := divRemConstPairEnabled
	defer func() { divRemConstPairEnabled = saved }()
	// MIT wasm.fyi source a7f8d8cc83f6074cd5c418928f0c2f9201114102.
	// License: ../../../../../../tests/fixtures/wasmfyi/LICENSE.
	// SHA-256 bca7073cd736412411d37597569095dc62e94c3d90a1a0a781ecf698bd7c173b.
	const fixture = "0061736d0100000001060160017f017f0303020000071b020962656e63686d61726b00000b706572666f726d616e636500010a4b022701027f03402002200141076e20014107706a6a2102200141016a210120012000490d000b20020b2101027f03402002200010006a2102200141016a2101200141c000490d000b20020b0059046e616d650115010012706572666f726d616e63655f6b65726e656c022602000300016e010169020161010300016e0109697465726174696f6e0208636865636b73756d031302000100046c6f6f7001010006726570656174"
	b, err := hex.DecodeString(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(b)); got != "bca7073cd736412411d37597569095dc62e94c3d90a1a0a781ecf698bd7c173b" {
		t.Fatalf("fixture SHA-256 = %s", got)
	}
	m, err := wasm.DecodeModule(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, on := range []bool{false, true} {
		divRemConstPairEnabled = on
		var stats ModuleStats
		cm, err := CompileModuleWith(m, CompileOptions{Stats: optionalTestStats(&stats)})
		if err != nil {
			t.Fatal(err)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		if diagnosticsEnabled && (stats.Funcs[0].Peephole["divrem-const-pair"] != 0) != on {
			t.Fatalf("on=%v hits=%v", on, stats.Funcs[0].Peephole)
		}
		if diagnosticsEnabled {
			t.Logf("bounds=explicit shared-enabled=%v shared-body=%v on=%v pair=%d", sharedScalarEnabled, stats.Funcs[0].SharedScalar, on, stats.Funcs[0].Peephole["divrem-const-pair"])
		}
		// The site calls export performance (function1), not benchmark (function0).
		cm.Entry[0] = cm.Entry[1]
		if got := runCompiledAmd64u(t, cm, 4096); got != 77351040 {
			t.Fatalf("on=%v checksum=%d", on, got)
		}
	}
}
