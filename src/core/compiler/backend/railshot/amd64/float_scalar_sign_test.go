//go:build linux && amd64

package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestFloatCopysignPreservesBits(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, op, reinterpret, xor := wasm.F32, byte(0x98), byte(0xbc), byte(0x73)
		ityp := wasm.I32
		values := []uint64{0, 0x80000000, 1, 0x80000001, 0x3f800000, 0xbf800000, 0x7f800000, 0xff800000, 0x7fc12345, 0x7f812345, 0xff812345}
		sign := uint64(1 << 31)
		if wide {
			typ, op, reinterpret, xor, ityp = wasm.F64, 0xa6, 0xbd, 0x85, wasm.I64
			sign = 1 << 63
			values = []uint64{0, 1 << 63, 1, 1<<63 | 1, 0x3ff0000000000000, 0xbff0000000000000, 0x7ff0000000000000, 0xfff0000000000000, 0x7ff8000000001234, 0x7ff0000000001234, 0xfff0000000001234}
		}
		for _, retain := range []bool{false, true} {
			body := []byte{0, 0x20, 0, 0x20, 1, op, reinterpret}
			if retain {
				body = append(body, 0x20, 0, reinterpret, xor, 0x20, 1, reinterpret, xor)
			}
			body = append(body, 0x0b)
			m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{ityp}, body)
			for _, a := range values {
				for _, b := range values {
					want := (a &^ sign) | (b & sign)
					if retain {
						want ^= a ^ b
					}
					got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{}, nil, a, b)
					if err != nil || got != want {
						t.Fatalf("wide=%v retain=%v a=%x b=%x got=%x want=%x err=%v", wide, retain, a, b, got, want, err)
					}
				}
			}
		}
	}
}

func TestFloatScalarSignPreservesBits(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, first, reinterpret, ityp := wasm.F32, byte(0x8b), byte(0xbc), wasm.I32
		sign := uint64(1 << 31)
		values := []uint64{0, 1 << 31, 1, 1<<31 | 1, 0x3f800000, 0xbf800000, 0x7f800000, 0xff800000, 0x7fc12345, 0x7f812345, 0xff812345}
		if wide {
			typ, first, reinterpret, ityp = wasm.F64, 0x99, 0xbd, wasm.I64
			sign = 1 << 63
			values = []uint64{0, 1 << 63, 1, 1<<63 | 1, 0x3ff0000000000000, 0xbff0000000000000, 0x7ff0000000000000, 0xfff0000000000000, 0x7ff8000000001234, 0x7ff0000000001234, 0xfff0000000001234}
		}
		for _, neg := range []bool{false, true} {
			op := first
			if neg {
				op++
			}
			m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{ityp}, []byte{0, 0x20, 0, op, reinterpret, 0x0b})
			for _, a := range values {
				want := a &^ sign
				if neg {
					want = a ^ sign
				}
				got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{}, nil, a)
				if err != nil || got != want {
					t.Fatalf("wide=%v neg=%v a=%x got=%x want=%x err=%v", wide, neg, a, got, want, err)
				}
			}
		}
	}
}

func TestFloatScalarSignLoadTrap(t *testing.T) {
	for _, wide := range []bool{false, true} {
		load, align, first, reinterpret, ityp := byte(0x2a), byte(2), byte(0x8b), byte(0xbc), wasm.I32
		copysign := byte(0x98)
		sign := uint64(1 << 31)
		if wide {
			load, align, first, reinterpret, ityp, copysign = 0x2b, 3, 0x99, 0xbd, wasm.I64, 0xa6
			sign = 1 << 63
		}
		for _, op := range []byte{first, first + 1, copysign} {
			body := []byte{0, 0x20, 0, load, align, 0}
			if op == copysign {
				body = append(body, 0x20, 1, load, align, 0)
			}
			body = append(body, op, reinterpret, 0x0b)
			m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{ityp}, body)
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
			want := uint64(0)
			if op == first+1 {
				want = sign
			}
			got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{}, nil, 0, 8)
			if err != nil || got != want {
				t.Fatalf("wide=%v op=%x valid got=%x want=%x err=%v", wide, op, got, want, err)
			}
			if _, _, err = runMemAmd64WithOptions(t, m, CompileOptions{}, nil, 65536, 8); err == nil {
				t.Fatalf("wide=%v op=%x: magnitude load did not trap", wide, op)
			}
			if op == copysign {
				if _, _, err = runMemAmd64WithOptions(t, m, CompileOptions{}, nil, 0, 65536); err == nil {
					t.Fatalf("wide=%v: sign load did not trap", wide)
				}
			}
		}
	}
}

func TestFloatScalarSignPendingLoads(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, load, align, reinterpret, ityp, first, xor := wasm.F32, byte(0x2a), byte(2), byte(0xbc), wasm.I32, byte(0x96), byte(0x73)
		a, b := uint64(0x40a00000), uint64(0xc0000000) // 5, -2
		if wide {
			typ, load, align, reinterpret, ityp, first, xor = wasm.F64, 0x2b, 3, 0xbd, wasm.I64, 0xa4, 0x85
			a, b = 0x4014000000000000, 0xc000000000000000
		}
		unary := byte(0x8b)
		if wide {
			unary = 0x99
		}
		for _, op := range []byte{first, first + 1, first + 2, unary, unary + 1} {
			body := []byte{1, 1, wasm.MustEncodeValType(ityp)}
			for i := byte(1); i <= 40; i++ {
				body = append(body, 0x20, 2, 0x41, i, 0x6c, load, align, 0)
			}
			body = append(body, 0x20, 0)
			if op >= first {
				body = append(body, 0x20, 1)
			}
			body = append(body, op, reinterpret, 0x21, 3)
			for range 40 {
				body = append(body, 0x1a)
			}
			body = append(body, 0x20, 3, 0x20, 0, reinterpret, xor, 0x20, 1, reinterpret, xor, 0x0b)
			m := mod1(t, []wasm.ValType{typ, typ, wasm.I32}, []wasm.ValType{ityp}, body)
			m.Memories = []wasm.MemType{{Limits: wasm.Limits{Min: 1}}}
			want := a
			switch op {
			case first:
				want = b
			case first + 2, unary + 1:
				if wide {
					want = a | 1<<63
				} else {
					want = a | 1<<31
				}
			}
			want ^= a ^ b
			got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{}, nil, a, b, 8)
			if err != nil || got != want {
				t.Fatalf("wide=%v op=%x got=%x want=%x err=%v", wide, op, got, want, err)
			}
		}
	}
}

func TestFloatScalarSignLocalSinkPreservesOldReferences(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, ityp, first, reinterpret, xor := wasm.F32, wasm.I32, byte(0x8b), byte(0xbc), byte(0x73)
		a, sign := uint64(0xff812345), uint64(1<<31)
		if wide {
			typ, ityp, first, reinterpret, xor = wasm.F64, wasm.I64, 0x99, 0xbd, 0x85
			a, sign = 0xfff0000000001234, 1<<63
		}
		for _, op := range []byte{first, first + 1} {
			for _, tee := range []bool{false, true} {
				store := byte(0x21)
				if tee {
					store = 0x22
				}
				body := []byte{1, 1, wasm.MustEncodeValType(ityp), 0x20, 0, 0x20, 0, op, store, 0}
				if tee {
					body = append(body, 0x1a)
				}
				body = append(body, 0x20, 0, reinterpret, 0x21, 1, reinterpret, 0x20, 1, xor, 0x0b)
				m := mod1(t, []wasm.ValType{typ}, []wasm.ValType{ityp}, body)
				var stats ModuleStats
				opts := CompileOptions{}
				if diagnosticsEnabled {
					opts.Stats = &stats
				}
				got, _, err := runMemAmd64WithOptions(t, m, opts, nil, a)
				if err != nil || got != sign {
					t.Fatalf("wide=%v op=%x tee=%v got=%x want=%x err=%v", wide, op, tee, got, sign, err)
				}
				if diagnosticsEnabled && stats.Funcs[0].Peephole["float-sign-local-sink"] != 1 {
					t.Fatalf("sink was not selected: %v", stats.Funcs[0].Peephole)
				}
			}
		}
	}
}

func TestFloatCopysignLocalSinkAliases(t *testing.T) {
	for _, wide := range []bool{false, true} {
		typ, ityp, op, reinterpret, xor := wasm.F32, wasm.I32, byte(0x98), byte(0xbc), byte(0x73)
		a, b := uint64(0x7f812345), uint64(0x80000001)
		if wide {
			typ, ityp, op, reinterpret, xor = wasm.F64, wasm.I64, 0xa6, 0xbd, 0x85
			a, b = 0x7ff0000000001234, 0x8000000000000001
		}
		for _, same := range []bool{false, true} {
			for _, dst := range []byte{0, 1} {
				for _, tee := range []bool{false, true} {
					right := byte(1)
					if same {
						right = 0
					}
					store := byte(0x21)
					if tee {
						store = 0x22
					}
					// Keep the old destination as a live float reference below the operands.
					body := []byte{1, 1, wasm.MustEncodeValType(ityp), 0x20, dst, 0x20, 0, 0x20, right, op, store, dst}
					if tee {
						body = append(body, 0x1a)
					}
					body = append(body, 0x20, dst, reinterpret, 0x21, 2, reinterpret, 0x20, 2, xor, 0x0b)
					m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{ityp}, body)
					sourceB := b
					if same {
						sourceB = a
					}
					old := a
					if dst == 1 {
						old = b
					}
					want := copysignBits(a, sourceB, wide) ^ old
					for _, features := range []shared.AMD64Features{0, shared.AMD64ModernBaseline} {
						var stats ModuleStats
						got, _, err := runMemAmd64WithOptions(t, m, CompileOptions{AMD64FeaturesSet: true, AMD64Features: features, Stats: optionalTestStats(&stats)}, nil, a, b)
						if err != nil || got != want {
							t.Fatalf("wide=%v same=%v dst=%d tee=%v features=%x got=%x want=%x err=%v", wide, same, dst, tee, features, got, want, err)
						}
						if diagnosticsEnabled && stats.Funcs[0].Peephole["float-copysign-local-sink"] != 1 {
							t.Fatalf("sink absent: %v", stats.Funcs[0].Peephole)
						}
					}
				}
			}
		}
	}
}
