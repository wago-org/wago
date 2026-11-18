//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"bytes"
	"errors"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	wr "github.com/wago-org/wago/src/core/runtime"
	"golang.org/x/sys/cpu"
	"math/bits"
	"reflect"
	"testing"
)

func streamRotateHomeEnable(t *testing.T) {
	t.Helper()
	streamGenerationTestEnable(t)
	a, b, c, d := streamingRotateHomeEnabled, streamingRorxEnabled, streamingPreserveDenseEnabled, streamingLocalLifetimesEnabled
	streamingRotateHomeEnabled, streamingRorxEnabled, streamingPreserveDenseEnabled, streamingLocalLifetimesEnabled = true, true, true, false
	t.Cleanup(func() {
		streamingRotateHomeEnabled, streamingRorxEnabled, streamingPreserveDenseEnabled, streamingLocalLifetimesEnabled = a, b, c, d
	})
}
func streamRotateHomeState() *streamState {
	s := streamResidencyUnitState()
	s.target.bmi2Rorx = true
	source := s.add(streamNode{kind: shared.ScalarRegister, reg: uint8(RAX), refs: 1, constant: 0})
	s.owners[RAX] = source
	count := s.add(streamNode{kind: shared.ScalarConstant, constant: 7, refs: 1})
	s.stack = []streamID{source, count}
	return s
}
func TestStreamRotateHomeCommitEncoding(t *testing.T) {
	streamRotateHomeEnable(t)
	s := streamRotateHomeState()
	old := s.locals[0]
	oldOrder := s.node(old).order
	home := s.residency.cells[s.residency.homes[0]].reg
	start := s.target.a.Len()
	r := wasm.ReaderFrom([]byte{0x21, 0})
	if !s.tryRotateIntoLocal(&r, 0x77) {
		t.Fatal("eligible rotate rejected")
	}
	if r.Offset() != 0 || len(s.stack) != 1 || s.locals[0] != 0 {
		t.Fatal("lookahead cursor or transfer state")
	}
	var want encoder.Asm
	want.Rorx(Reg(home), RAX, 25, false)
	if !bytes.Equal(s.target.a.B[start:], want.B) {
		t.Fatalf("native %x want %x", s.target.a.B[start:], want.B)
	}
	id := s.stack[0]
	s.stack = nil
	if s.node(id).kind != streamBacking || s.node(id).order <= oldOrder || s.node(id).constant == 0 {
		t.Fatal("fresh generation/token missing")
	}
	before := s.target.a.Len()
	if !s.commitRotateIntoLocal(0, id, 0) {
		t.Fatal("token commit rejected")
	}
	if s.locals[0] != id || s.node(id).constant != 0 || s.target.a.Len() != before {
		t.Fatal("commit failed or emitted redundant move")
	}
	if s.commitRotateIntoLocal(0, id, 0) {
		t.Fatal("consumed token replayed")
	}
}
func TestStreamRotateHomePreflightPurity(t *testing.T) {
	streamRotateHomeEnable(t)
	for _, why := range []string{"local-get", "variable", "old-alias", "cell-alias", "copied-proof-alias", "same-old-source", "nonhome", "stale-map"} {
		t.Run(why, func(t *testing.T) {
			s := streamRotateHomeState()
			code := []byte{0x21, 0}
			old := s.locals[0]
			switch why {
			case "local-get":
				code[0] = 0x20
			case "variable":
				s.node(s.stack[1]).kind = shared.ScalarFrame
				s.node(s.stack[1]).slot = 9
			case "old-alias":
				s.retain(old)
			case "cell-alias":
				s.borrowBacking(s.residency.homes[0], 0)
			case "copied-proof-alias":
				alias := s.borrowBacking(s.residency.homes[0], 0)
				s.materialize(alias, 0)
			case "same-old-source":
				s.release(s.stack[0])
				s.retain(old)
				s.stack[0] = old
			case "nonhome":
				code[1] = 2
			case "stale-map":
				s.residency.homes[0] = 0
			}
			r := wasm.ReaderFrom(code)
			nodes := append([]streamNode{}, s.nodes...)
			cells := append([]streamBackingCell{}, s.residency.cells...)
			locals := append([]streamID{}, s.locals...)
			stack := append([]streamID{}, s.stack...)
			owners := s.owners
			native := append([]byte{}, s.target.a.B...)
			if s.tryRotateIntoLocal(&r, 0x77) {
				t.Fatal("unsafe admission", why)
			}
			if r.Offset() != 0 || !reflect.DeepEqual(nodes, s.nodes) || !reflect.DeepEqual(cells, s.residency.cells) || !reflect.DeepEqual(locals, s.locals) || !reflect.DeepEqual(stack, s.stack) || owners != s.owners || !bytes.Equal(native, s.target.a.B) {
				t.Fatal("preflight mutation", why)
			}
		})
	}
}
func TestStreamRotateHomeTokenRejection(t *testing.T) {
	streamRotateHomeEnable(t)
	for _, why := range []string{"order", "token-local", "map", "cell-alias", "old-nonzero"} {
		t.Run(why, func(t *testing.T) {
			s := streamRotateHomeState()
			r := wasm.ReaderFrom([]byte{0x21, 0})
			if !s.tryRotateIntoLocal(&r, 0x77) {
				t.Fatal("prepare")
			}
			id := s.stack[0]
			s.stack = nil
			old := streamID(0)
			switch why {
			case "order":
				s.node(id).order++
			case "token-local":
				s.node(id).constant ^= 1 << 32
			case "map":
				s.residency.homes[0] = 0
			case "cell-alias":
				s.borrowBacking(uint32(s.node(id).slot), 0)
			case "old-nonzero":
				old = id
			}
			before := s.target.a.Len()
			token := s.node(id).constant
			if s.commitRotateIntoLocal(0, id, old) {
				t.Fatal("stale handoff accepted", why)
			}
			if s.target.a.Len() != before || s.node(id).constant != token || s.locals[0] != 0 {
				t.Fatal("rejected commit mutated state")
			}
		})
	}
}
func TestStreamRotateHomeRuntime(t *testing.T) {
	if !cpu.X86.HasBMI2 {
		t.Skip("native RORX requires BMI2")
	}
	streamRotateHomeEnable(t)
	for _, op := range []byte{0x77, 0x78, 0x89, 0x8a} {
		wide, _ := streamRotateShape(op)
		typ := streamNumericType(wide)
		xor := byte(0x73)
		if wide {
			xor = 0x85
		}
		for _, alias := range []bool{false, true} {
			body := []byte{0}
			for i := 0; i < 100; i++ {
				body = append(body, 1)
			}
			blockType := byte(0x7f)
			if wide {
				blockType = 0x7e
			}
			body = append(body, 0x03, blockType)
			if alias {
				body = append(body, 0x20, 0)
			}
			body = append(body, 0x20, 0, 0x20, 1, xor)
			body = append(body, streamConstBytes(wide, 7)...)
			body = append(body, op, 0x21, 0, 0x20, 0)
			if alias {
				body = append(body, xor)
			}
			body = append(body, 0x0b, 0x0b)
			m := mod1(t, []wasm.ValType{typ, typ}, []wasm.ValType{typ}, body)
			for _, on := range []bool{false, true} {
				streamingRotateHomeEnabled = on
				for _, intr := range []bool{false, true} {
					var stats ModuleStats
					opts := CompileOptions{Interruptible: intr, AMD64FeaturesSet: true, AMD64Features: shared.AMD64BMI2, Optimizations: map[string]bool{"bmi2-rorx": true}}
					if diagnosticsEnabled {
						opts.Stats = &stats
					}
					x, y := uint64(0xdeadbeef87654321), uint64(0xabcdef0112345678)
					n := 7
					if op == 0x78 || op == 0x8a {
						n = -n
					}
					want := bits.RotateLeft64(x^y, n)
					if !wide {
						want = uint64(bits.RotateLeft32(uint32(x^y), n))
					}
					if alias {
						if wide {
							want ^= x
						} else {
							want ^= uint64(uint32(x))
						}
					}
					got, _, err := runMemAmd64WithOptions(t, m, opts, nil, x, y)
					if err != nil || got != want {
						t.Fatalf("op%x alias%v on%v intr%v got%x want%x err%v", op, alias, on, intr, got, want, err)
					}
					if diagnosticsEnabled && !regallocCheckEnabled {
						p := stats.Funcs[0].Peephole
						if p["streaming-function"] != 1 || p["stream-residency-home"] == 0 {
							t.Fatal("fallback or no home", p)
						}
						if on && !alias {
							if p["stream-rotate-home"] != 1 || p["stream-rotate-home-set"] != 1 {
								t.Fatal("positive rotate handoff absent", p)
							}
						} else if p["stream-rotate-home"] != 0 {
							t.Fatal("alias/OFF unexpectedly selected", p)
						}
					}
				}
			}
		}
	}
}
func TestStreamRotateHomeEagerTrapPC(t *testing.T) {
	if !cpu.X86.HasBMI2 {
		t.Skip("native RORX requires BMI2")
	}
	streamRotateHomeEnable(t)
	for _, load := range []bool{false, true} {
		body := []byte{0, 0x03, 0x40}
		body = append(body, 0x20, 1)
		if !load {
			body = append(body, 0x20, 0)
		}
		pc := uint32(len(body))
		code := wr.TrapDivZero
		args := []uint64{0, 9}
		if load {
			body = append(body, 0x28, 0, 0)
			code = wr.TrapLinMemOutOfBounds
			args = []uint64{0, 65535}
		} else {
			body = append(body, 0x6d)
		}
		body = append(body, 0x41, 7, 0x77, 0x21, 0, 0x00, 0x0b, 0x0b)
		m := modMem(t, 1, []wasm.ValType{wasm.I32, wasm.I32}, nil, body)
		for _, on := range []bool{false, true} {
			streamingRotateHomeEnabled = on
			for _, intr := range []bool{false, true} {
				opts := CompileOptions{Interruptible: intr, AMD64FeaturesSet: true, AMD64Features: shared.AMD64BMI2, Optimizations: map[string]bool{"bmi2-rorx": true}}
				_, _, err := runMemAmd64WithOptions(t, m, opts, nil, args...)
				var trap *wr.TrapError
				if !errors.As(err, &trap) || trap.Code != code || len(trap.Frames) != 1 || trap.Frames[0].ProgramCounter != pc {
					t.Fatalf("load%v on%v intr%v trap%+v expectedcode%v pc%d", load, on, intr, err, code, pc)
				}
			}
		}
	}
}
