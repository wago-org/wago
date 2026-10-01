//go:build (linux || darwin || windows) && (amd64 || arm64) && !tinygo

package wago

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func guardedEffectPressureModule(wide bool, op byte, loads int, outOfBounds, loadFirst bool) []byte {
	typ, load, add := wasm.I32, byte(0x28), byte(0x6a)
	if wide {
		typ, load, add = wasm.I64, 0x29, 0x7c
	}
	address := []byte{0x41, 0}
	if outOfBounds {
		address = []byte{0x41, 0x80, 0x80, 0x04} // i32.const 65536
	}
	var body []byte
	appendLoad := func() {
		body = append(body, address...)
		body = append(body, load, 0, 0)
	}
	if loadFirst {
		appendLoad()
	}
	body = append(body, 0x20, 0, 0x20, 1, op)
	for range loads {
		appendLoad()
	}
	body = append(body, 0x41, 16, 0x41, 7, 0x36, 2, 0)
	for range loads {
		body = append(body, add)
	}
	if loadFirst {
		body = append(body, add)
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ, typ}, []wasm.ValType{typ}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestDeferredEffectsGuardLoadPressure(t *testing.T) {
	if !GuardPageSupported() {
		t.Skip("guard-page build required")
	}
	for _, wide := range []bool{false, true} {
		first, min, minusOne := byte(0x6d), uint64(1<<31), uint64(0xffffffff)
		if wide {
			first, min, minusOne = 0x7f, 1<<63, ^uint64(0)
		}
		for offset, name := range []string{"div_s", "div_u", "rem_s", "rem_u"} {
			for _, n := range []int{1, 8, 10, 24} {
				for _, outOfBounds := range []bool{false, true} {
					for _, loadFirst := range []bool{false, true} {
						t.Run(fmt.Sprintf("wide=%v/%s/loads=%d/oob=%v/load_first=%v", wide, name, n, outOfBounds, loadFirst), func(t *testing.T) {
							module := guardedEffectPressureModule(wide, first+byte(offset), n, outOfBounds, loadFirst)
							compiled := compileDeferredEffect(t, module, BoundsChecksSignalsBased)
							cases := []struct {
								x, y uint64
								trap TrapCode
							}{{42, 7, 0}, {42, 0, TrapDivZero}}
							if offset == 0 {
								cases = append(cases, struct {
									x, y uint64
									trap TrapCode
								}{min, minusOne, TrapDivOverflow})
							}
							for _, tc := range cases {
								wantTrap := tc.trap
								if outOfBounds && (loadFirst || wantTrap == 0) {
									wantTrap = TrapLinMemOutOfBounds
								}
								instance := instantiateDeferredEffect(t, compiled)
								got, err := instance.Invoke("run", tc.x, tc.y)
								stored := binary.LittleEndian.Uint64(instance.Memory().UnsafeBytes()[16:24])
								if wantTrap != 0 {
									var trap *TrapError
									if !errors.As(err, &trap) || trap.Code != wantTrap || stored != 0 {
										t.Errorf("args=%d,%d: result %v, error %v, stored %d; want trap %v before store", tc.x, tc.y, got, err, stored, wantTrap)
									}
								} else {
									want := uint64(6)
									if offset >= 2 {
										want = 0
									}
									if err != nil || len(got) != 1 || got[0] != want || stored != 7 {
										t.Errorf("result %v, error %v, stored %d; want %d, nil, 7", got, err, stored, want)
									}
								}
							}
						})
					}
				}
			}
		}
	}
}

func BenchmarkGuardedDeferredEffects(b *testing.B) {
	if !GuardPageSupported() {
		b.Skip("guard-page build required")
	}
	for _, tc := range []struct {
		name  string
		op    byte
		loads int
		want  uint64
	}{{"div", 0x80, 0, 6}, {"div_loads", 0x80, 8, 6}, {"pure_loads", 0x7c, 8, 49}} {
		module := guardedEffectPressureModule(true, tc.op, tc.loads, false, false)
		b.Run(tc.name+"/compile", func(b *testing.B) {
			cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksSignalsBased)
			var codeBytes int
			b.ReportAllocs()
			for range b.N {
				compiled, err := Compile(cfg, module)
				if err != nil {
					b.Fatal(err)
				}
				codeBytes = compiled.CodeSize()
				compiled.Close()
			}
			b.ReportMetric(float64(codeBytes), "code-B")
		})
		b.Run(tc.name+"/invoke", func(b *testing.B) {
			compiled := compileDeferredEffect(b, module, BoundsChecksSignalsBased)
			instance := instantiateDeferredEffect(b, compiled)
			got, err := instance.Invoke("run", 42, 7)
			if err != nil || len(got) != 1 || got[0] != tc.want {
				b.Fatalf("result %v, error %v; want %d", got, err, tc.want)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := instance.Invoke("run", 42, 7); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(compiled.CodeSize()), "code-B")
		})
	}
}
