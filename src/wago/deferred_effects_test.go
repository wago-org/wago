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

// deferredEffectModule keeps a div/rem (also inside an add) live below a later
// side effect. An explicit return discards that live prefix instead.
func deferredEffectModule(wide bool, op byte, effect string, load bool) []byte {
	typ, constant, add := wasm.I32, byte(0x41), byte(0x6a)
	if wide {
		typ, constant, add = wasm.I64, 0x42, 0x7c
	}
	params := []wasm.ValType{typ, typ}
	body := []byte{0x20, 0, 0x20, 1, op}
	if load {
		params = []wasm.ValType{wasm.I32}
		body = []byte{0x20, 0, 0x28, 2, 0} // i32.load
	}
	body = append(body, constant, 3, add)
	switch effect {
	case "store":
		body = append(body, 0x41, 16, 0x41, 7, 0x36, 2, 0)
	case "store_f32":
		body = append(body, 0x41, 16, 0x43, 0, 0, 0xe0, 0x40, 0x38, 2, 0)
	case "store_f64":
		body = append(body, 0x41, 16, 0x44, 0, 0, 0, 0, 0, 0, 0x1c, 0x40, 0x39, 3, 0)
	case "store_v128", "store_lane":
		body = append(body, 0x41, 16, 0xfd, 12, 7)
		body = append(body, make([]byte, 15)...)
		if effect == "store_v128" {
			body = append(body, 0xfd, 11, 4, 0)
		} else {
			body = append(body, 0xfd, 0x5b, 3, 0, 0)
		}
	case "store_value":
		// A second div/rem in the value makes both operand trees observable.
		body = append(body, 0x41, 16, constant, 14, 0x20, 1, op)
		store := byte(0x36)
		if wide {
			store = 0x37
		}
		body = append(body, store, 2, 0)
	case "fill":
		body = append(body, 0x41, 16, 0x41, 7, 0x41, 4, 0xfc, 11, 0)
	case "global_i32":
		body = append(body, 0x41, 7, 0x24, 0)
	case "global_f64":
		body = append(body, 0x44, 0, 0, 0, 0, 0, 0, 0x1c, 0x40, 0x24, 1)
	case "grow":
		body = append(body, 0x41, 1, 0x40, 0, 0x1a)
	case "return":
		body = append(body, constant, 7, 0x0f)
	default:
		panic("unknown deferred effect")
	}
	body = append(body, 0x0b)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(params, []wasm.ValType{typ}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{1, 1, 2})),
		wasmtest.Section(6, wasmtest.Vec(
			[]byte{0x7f, 1, 0x41, 0, 0x0b},
			[]byte{0x7c, 1, 0x44, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b},
		)),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("run", 0, 0), wasmtest.ExportEntry("memory", 2, 0),
			wasmtest.ExportEntry("g32", 3, 0), wasmtest.ExportEntry("g64", 3, 1),
		)),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func compileDeferredEffect(t testing.TB, module []byte, mode BoundsCheckMode) *Compiled {
	t.Helper()
	compiled, err := Compile(NewRuntimeConfig().WithBoundsChecks(mode), module)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { compiled.Close() })
	return compiled
}

func instantiateDeferredEffect(t testing.TB, compiled *Compiled) *Instance {
	t.Helper()
	instance, err := Instantiate(compiled)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { instance.Close() })
	return instance
}

func checkDeferredEffectState(t *testing.T, instance *Instance, effect string, store uint64, success bool) {
	t.Helper()
	wantStore, wantPages, want32, want64 := uint64(0), 1, uint64(0), uint64(0)
	if success {
		switch effect {
		case "store", "store_value", "store_v128", "store_lane":
			wantStore = store
		case "store_f32":
			wantStore = F32(7)
		case "store_f64":
			wantStore = F64(7)
		case "fill":
			wantStore = 0x07070707
		case "global_i32":
			want32 = 7
		case "global_f64":
			want64 = F64(7)
		case "grow":
			wantPages = 2
		}
	}
	mem := instance.Memory().UnsafeBytes()
	if got := binary.LittleEndian.Uint64(mem[16:24]); got != wantStore {
		t.Errorf("memory changed: got %#x, want %#x", got, wantStore)
	}
	if len(mem) != wantPages*65536 {
		t.Errorf("memory size = %d, want %d", len(mem), wantPages*65536)
	}
	for name, want := range map[string]uint64{"g32": want32, "g64": want64} {
		if got, err := instance.Global(name); err != nil || got != want {
			t.Errorf("global %s = %#x, %v; want %#x", name, got, err, want)
		}
	}
}

func TestDeferredDivRemBeforeEffects(t *testing.T) {
	for _, wide := range []bool{false, true} {
		first, min, minusOne := byte(0x6d), uint64(1<<31), uint64(0xffffffff)
		if wide {
			first, min, minusOne = 0x7f, 1<<63, ^uint64(0)
		}
		for offset, name := range []string{"div_s", "div_u", "rem_s", "rem_u"} {
			for _, effect := range []string{"store", "store_value", "store_f32", "store_f64", "store_v128", "store_lane", "fill", "global_i32", "global_f64", "grow", "return"} {
				t.Run(fmt.Sprintf("wide=%v/%s/%s", wide, name, effect), func(t *testing.T) {
					compiled := compileDeferredEffect(t, deferredEffectModule(wide, first+byte(offset), effect, false), BoundsChecksExplicit)
					cases := []struct {
						name string
						x, y uint64
						trap TrapCode
					}{{"zero", 1, 0, TrapDivZero}, {"success", 42, 7, 0}}
					if offset == 0 {
						cases = append(cases, struct {
							name string
							x, y uint64
							trap TrapCode
						}{"overflow", min, minusOne, TrapDivOverflow})
					}
					for _, tc := range cases {
						t.Run(tc.name, func(t *testing.T) {
							instance := instantiateDeferredEffect(t, compiled)
							got, err := instance.Invoke("run", tc.x, tc.y)
							success := tc.name == "success"
							store := uint64(7)
							if effect == "store_value" {
								store = 2
								if offset >= 2 {
									store = 0
								}
							}
							checkDeferredEffectState(t, instance, effect, store, success)
							if !success {
								var trap *TrapError
								if !errors.As(err, &trap) || trap.Code != tc.trap {
									t.Errorf("Invoke = %v, %v; want trap %v", got, err, tc.trap)
								}
								return
							}
							want := uint64(9)
							if offset >= 2 {
								want = 3
							}
							if effect == "return" {
								want = 7
							}
							if err != nil || len(got) != 1 || got[0] != want {
								t.Errorf("Invoke = %v, %v; want %d", got, err, want)
							}
						})
					}
				})
			}
		}
	}
}

func TestDeferredLoadBeforeEffects(t *testing.T) {
	modes := []BoundsCheckMode{BoundsChecksExplicit}
	if GuardPageSupported() {
		modes = append(modes, BoundsChecksSignalsBased)
	}
	for _, mode := range modes {
		for _, effect := range []string{"store", "store_f32", "store_f64", "store_v128", "store_lane", "global_i32", "global_f64", "grow", "return"} {
			t.Run(fmt.Sprintf("bounds=%d/%s", mode, effect), func(t *testing.T) {
				compiled := compileDeferredEffect(t, deferredEffectModule(false, 0, effect, true), mode)
				instance := instantiateDeferredEffect(t, compiled)
				// Growing by one page would make this address accessible too late.
				got, err := instance.Invoke("run", 65536)
				checkDeferredEffectState(t, instance, effect, 7, false)
				var trap *TrapError
				if !errors.As(err, &trap) || trap.Code != TrapLinMemOutOfBounds {
					t.Errorf("Invoke = %v, %v; want out-of-bounds trap", got, err)
				}
			})
		}
	}
}

func TestDeferredEffectsPressure(t *testing.T) {
	// Many live quotients force spills while the ordered walk condenses each
	// division. The store operands and every later sum must keep their values.
	body := make([]byte, 0, 256)
	for range 24 {
		body = append(body, 0x20, 0, 0x20, 1, 0x80) // i64.div_u
	}
	body = append(body, 0x41, 16, 0x42, 7, 0x37, 3, 0)
	for range 23 {
		body = append(body, 0x7c)
	}
	body = append(body, 0x0b)
	module := wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I64, wasm.I64}, []wasm.ValType{wasm.I64}))),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0))),
		wasmtest.Section(5, wasmtest.Vec([]byte{0, 1})),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
	compiled := compileDeferredEffect(t, module, BoundsChecksExplicit)
	for _, divisor := range []uint64{0, 7} {
		instance := instantiateDeferredEffect(t, compiled)
		got, err := instance.Invoke("run", 42, divisor)
		stored := binary.LittleEndian.Uint64(instance.Memory().UnsafeBytes()[16:24])
		if divisor == 0 {
			var trap *TrapError
			if !errors.As(err, &trap) || trap.Code != TrapDivZero || stored != 0 {
				t.Errorf("zero divisor: result %v, error %v, stored %d", got, err, stored)
			}
		} else if err != nil || len(got) != 1 || got[0] != 144 || stored != 7 {
			t.Errorf("success: result %v, error %v, stored %d", got, err, stored)
		}
	}
}

func BenchmarkDeferredEffects(b *testing.B) {
	for _, effect := range []string{"store", "pure_store", "global_f64", "return"} {
		op, target := byte(0x80), effect
		if effect == "pure_store" {
			op, target = 0x7c, "store" // same shape with non-trapping i64.add
		}
		module := deferredEffectModule(true, op, target, false)
		b.Run(effect+"/compile", func(b *testing.B) {
			cfg := NewRuntimeConfig().WithBoundsChecks(BoundsChecksExplicit)
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
		b.Run(effect+"/invoke", func(b *testing.B) {
			compiled := compileDeferredEffect(b, module, BoundsChecksExplicit)
			instance := instantiateDeferredEffect(b, compiled)
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
