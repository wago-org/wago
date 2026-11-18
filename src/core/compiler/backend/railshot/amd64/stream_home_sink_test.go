//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"reflect"
	"testing"
)

func streamHomeSinkState(op shared.IntOp, register bool) (*streamState, streamID, streamID) {
	s := streamHomeUnitState(0)
	old := s.locals[0]
	s.retain(old)
	var right streamID
	if register {
		right = s.locals[1]
		s.retain(right)
	} else {
		right = s.add(streamNode{kind: shared.ScalarConstant, constant: 2, refs: 1})
	}
	id := s.add(streamNode{kind: streamDeferred, op: op, left: old, right: right, refs: 1})
	return s, id, old
}
func TestStreamHomeSinkOwnership(t *testing.T) {
	oldFlag := streamingHomeSinkEnabled
	streamingHomeSinkEnabled = true
	defer func() { streamingHomeSinkEnabled = oldFlag }()
	for _, op := range []shared.IntOp{shared.IntAdd, shared.IntSub, shared.IntAnd, shared.IntOr, shared.IntXor} {
		for _, reg := range []bool{false, true} {
			s, id, old := streamHomeSinkState(op, reg)
			before := s.target.a.Len()
			if !s.tryHomeSet(0, id, old) {
				t.Fatalf("eligible op=%v register=%v rejected", op, reg)
			}
			n := s.node(id)
			if s.locals[0] != id || n.kind != streamBorrow || n.refs != 1 || n.home != s.localHomeTag(0) || n.left != 0 || n.right != 0 || s.node(old).refs != 0 || s.target.a.Len() <= before {
				t.Fatal("sink ownership or emission invalid")
			}
			for _, r := range s.homeRegs {
				if s.owners[r] != 0 {
					t.Fatal("home unexpectedly owns node")
				}
			}
			if reg && s.node(s.locals[1]).refs != 1 {
				t.Fatal("RHS local lost ownership")
			}
		}
	}
	for _, why := range []string{"old-alias", "expression-alias", "unsupported-mul", "wide-immediate"} {
		s, id, old := streamHomeSinkState(shared.IntSub, false)
		switch why {
		case "old-alias":
			s.retain(old)
		case "expression-alias":
			s.retain(id)
		case "unsupported-mul":
			s.node(id).op = shared.IntMul
		case "wide-immediate":
			s.node(id).operandWide = true
			s.node(id).wide = true
			s.node(s.node(id).right).constant = 1 << 40
		}
		nodes := append([]streamNode{}, s.nodes...)
		locals := append([]streamID{}, s.locals...)
		owners := s.owners
		code := append([]byte{}, s.target.a.B...)
		if s.tryHomeSet(0, id, old) {
			t.Fatal("unsafe sink accepted", why)
		}
		if !reflect.DeepEqual(nodes, s.nodes) || !reflect.DeepEqual(locals, s.locals) || owners != s.owners || !bytes.Equal(code, s.target.a.B) {
			t.Fatal("failed sink mutated state", why)
		}
	}
}
func TestStreamHomeSinkRuntime(t *testing.T) {
	oldStream, oldHomes, oldSink := streamingOperandsEnabled, streamingRegisterHomesEnabled, streamingHomeSinkEnabled
	streamingOperandsEnabled, streamingRegisterHomesEnabled = true, true
	defer func() {
		streamingOperandsEnabled, streamingRegisterHomesEnabled, streamingHomeSinkEnabled = oldStream, oldHomes, oldSink
	}()
	for _, op := range []byte{0x6a, 0x6b, 0x71, 0x72, 0x73} {
		for _, register := range []bool{false, true} {
			for _, alias := range []bool{false, true} {
				body := []byte{0, 0x03, 0x7f}
				if alias {
					body = append(body, 0x20, 0)
				}
				body = append(body, 0x20, 0)
				rhs := uint32(2)
				if register {
					body = append(body, 0x20, 1)
					rhs = 3
				} else {
					body = append(body, 0x41, 2)
				}
				body = append(body, op, 0x21, 0, 0x20, 0)
				if alias {
					body = append(body, 0x6a)
				}
				body = append(body, 0x0b, 0x0b)
				m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
				want := uint32(7)
				switch op {
				case 0x6a:
					want += rhs
				case 0x6b:
					want -= rhs
				case 0x71:
					want &= rhs
				case 0x72:
					want |= rhs
				case 0x73:
					want ^= rhs
				}
				if alias {
					want += 7
				}
				for _, sink := range []bool{false, true} {
					streamingHomeSinkEnabled = sink
					for _, intr := range []bool{false, true} {
						opts := streamNumericControlCompile(t, m, intr)
						cm, e := CompileModuleWith(m, opts)
						if e != nil {
							t.Fatal(e)
						}
						if cm.CodeImage != nil {
							defer cm.CodeImage.Close()
						}
						streamNumericControlSelected(t, opts)
						if diagnosticsEnabled && !regallocCheckEnabled {
							n := opts.Stats.Funcs[0].Peephole["stream-home-sink"]
							if sink && !alias && n == 0 || (!sink || alias) && n != 0 {
								t.Fatalf("op%x reg%v alias%v sink%v selected%d", op, register, alias, sink, n)
							}
						}
						if got := runCompiledAmd64u(t, cm, 7, 3); got != uint64(want) {
							t.Fatalf("op%x reg%v alias%v sink%v intr%v got%d want%d", op, register, alias, sink, intr, got, want)
						}
					}
				}
			}
		}
	}
}

func TestStreamHomeSinkDetachedCompare(t *testing.T) {
	oldStream, oldHomes, oldSink := streamingOperandsEnabled, streamingRegisterHomesEnabled, streamingHomeSinkEnabled
	streamingOperandsEnabled, streamingRegisterHomesEnabled, streamingHomeSinkEnabled = true, true, true
	defer func() {
		streamingOperandsEnabled, streamingRegisterHomesEnabled, streamingHomeSinkEnabled = oldStream, oldHomes, oldSink
	}()
	// The pending compare retains the old selected-home value while local.set
	// installs a new value. It must not become a comparison of the new home.
	body := []byte{0, 0x03, 0x7f, 0x20, 0, 0x20, 1, 0x49, 0x20, 0, 0x41, 2, 0x6a, 0x21, 0, 0x04, 0x7f, 0x20, 0, 0x05, 0x20, 1, 0x0b, 0x0b, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, intr := range []bool{false, true} {
		opts := streamNumericControlCompile(t, m, intr)
		cm, e := CompileModuleWith(m, opts)
		if e != nil {
			t.Fatal(e)
		}
		if cm.CodeImage != nil {
			defer cm.CodeImage.Close()
		}
		streamNumericControlSelected(t, opts)
		if diagnosticsEnabled && !regallocCheckEnabled {
			stats := opts.Stats.Funcs[0].Peephole
			if stats["stream-local-home"] != 2 || stats["stream-home-sink"] != 0 {
				t.Fatal("must retain two homes without unsafe alias sink", stats)
			}
		}
		if got := runCompiledAmd64u(t, cm, 10, 11); got != 12 {
			t.Fatalf("detached compare got%d want12", got)
		}
	}
}

func TestStreamHomeSinkConditionalBackedge(t *testing.T) {
	oldStream, oldHomes, oldSink := streamingOperandsEnabled, streamingRegisterHomesEnabled, streamingHomeSinkEnabled
	streamingOperandsEnabled, streamingRegisterHomesEnabled = true, true
	defer func() {
		streamingOperandsEnabled, streamingRegisterHomesEnabled, streamingHomeSinkEnabled = oldStream, oldHomes, oldSink
	}()
	body := []byte{1, 1, 0x7f, 0x03, 0x40, 0x20, 1, 0x20, 0, 0x6a, 0x21, 1, 0x20, 0, 0x41, 1, 0x6b, 0x21, 0, 0x20, 0, 0x41, 0, 0x4b, 0x0d, 0, 0x0b, 0x20, 1, 0x0b}
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, body)
	for _, on := range []bool{false, true} {
		streamingHomeSinkEnabled = on
		for _, intr := range []bool{false, true} {
			opts := streamNumericControlCompile(t, m, intr)
			cm, e := CompileModuleWith(m, opts)
			if e != nil {
				t.Fatal(e)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			streamNumericControlSelected(t, opts)
			if diagnosticsEnabled && !regallocCheckEnabled {
				stats := opts.Stats.Funcs[0].Peephole
				if stats["stream-local-home"] != 2 {
					t.Fatal("requires both selected homes", stats)
				}
				if on && stats["stream-home-branch"] != 1 || !on && stats["stream-home-branch"] != 0 {
					t.Fatal("direct compare branch selection", stats)
				}
			}
			for _, n := range []uint64{1, 2, 20} {
				if got, want := runCompiledAmd64u(t, cm, n), n*(n+1)/2; got != want {
					t.Fatalf("sink%v intr%v n%d got%d want%d", on, intr, n, got, want)
				}
			}
		}
	}
}
func TestStreamHomeSinkBooleanBranches(t *testing.T) {
	oldStream, oldHomes, oldSink := streamingOperandsEnabled, streamingRegisterHomesEnabled, streamingHomeSinkEnabled
	streamingOperandsEnabled, streamingRegisterHomesEnabled = true, true
	defer func() {
		streamingOperandsEnabled, streamingRegisterHomesEnabled, streamingHomeSinkEnabled = oldStream, oldHomes, oldSink
	}()
	cases := []struct {
		name   string
		params int
		body   []byte
		args   []uint64
		want   uint64
	}{
		{"tee-boolean-backedge", 1, []byte{0, 0x03, 0x40, 0x20, 0, 0x41, 1, 0x6b, 0x22, 0, 0x0d, 0, 0x0b, 0x20, 0, 0x0b}, []uint64{5}, 0},
		{"block-true-exit", 1, []byte{1, 1, 0x7f, 0x03, 0x7f, 0x02, 0x40, 0x20, 0, 0x0d, 0, 0x41, 7, 0x21, 1, 0x0b, 0x20, 1, 0x0b, 0x0b}, []uint64{1}, 0},
		{"block-false-fallthrough", 1, []byte{1, 1, 0x7f, 0x03, 0x7f, 0x02, 0x40, 0x20, 0, 0x0d, 0, 0x41, 7, 0x21, 1, 0x0b, 0x20, 1, 0x0b, 0x0b}, []uint64{0}, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ps := make([]wasm.ValType, tc.params)
			for i := range ps {
				ps[i] = wasm.I32
			}
			m := mod1(t, ps, []wasm.ValType{wasm.I32}, tc.body)
			for _, on := range []bool{false, true} {
				streamingHomeSinkEnabled = on
				for _, intr := range []bool{false, true} {
					opts := streamNumericControlCompile(t, m, intr)
					cm, e := CompileModuleWith(m, opts)
					if e != nil {
						t.Fatal(e)
					}
					if cm.CodeImage != nil {
						defer cm.CodeImage.Close()
					}
					streamNumericControlSelected(t, opts)
					if diagnosticsEnabled && !regallocCheckEnabled {
						p := opts.Stats.Funcs[0].Peephole
						if p["stream-local-home"] == 0 {
							t.Fatal("home fallback")
						}
						if on && p["stream-home-branch"] == 0 || !on && p["stream-home-branch"] != 0 {
							t.Fatalf("on%v counter=%v", on, p)
						}
					}
					if got := runCompiledAmd64u(t, cm, tc.args...); got != tc.want {
						t.Fatalf("on%v intr%v got%d want%d", on, intr, got, tc.want)
					}
				}
			}
		})
	}
}

func TestStreamHomeSinkRootExits(t *testing.T) {
	oldStream, oldHomes, oldSink := streamingOperandsEnabled, streamingRegisterHomesEnabled, streamingHomeSinkEnabled
	streamingOperandsEnabled, streamingRegisterHomesEnabled, streamingHomeSinkEnabled = true, true, true
	defer func() {
		streamingOperandsEnabled, streamingRegisterHomesEnabled, streamingHomeSinkEnabled = oldStream, oldHomes, oldSink
	}()
	for _, tc := range []struct {
		name      string
		body      []byte
		results   []wasm.ValType
		arg, want uint64
	}{
		{"root-void", []byte{0, 0x01, 0x0b}, nil, 0, 0},
		{"root-result", []byte{0, 0x20, 0, 0x41, 3, 0x6a, 0x0b}, []wasm.ValType{wasm.I32}, 9, 12},
		{"function-branch-join", []byte{0, 0x41, 17, 0x20, 0, 0x0d, 0, 0x1a, 0x41, 19, 0x0b}, []wasm.ValType{wasm.I32}, 1, 17},
		{"function-branch-fallthrough", []byte{0, 0x41, 17, 0x20, 0, 0x0d, 0, 0x1a, 0x41, 19, 0x0b}, []wasm.ValType{wasm.I32}, 0, 19},
		{"early-return", []byte{0, 0x20, 0, 0x04, 0x40, 0x41, 11, 0x0f, 0x0b, 0x41, 13, 0x0b}, []wasm.ValType{wasm.I32}, 1, 11},
		{"early-return-fallthrough", []byte{0, 0x20, 0, 0x04, 0x40, 0x41, 11, 0x0f, 0x0b, 0x41, 13, 0x0b}, []wasm.ValType{wasm.I32}, 0, 13},
		{"retained-local-alias", []byte{0, 0x20, 0, 0x41, 9, 0x21, 0, 0x20, 0, 0x6a, 0x0b}, []wasm.ValType{wasm.I32}, 7, 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := modMem(t, 1, []wasm.ValType{wasm.I32}, tc.results, tc.body)
			for _, intr := range []bool{false, true} {
				opts := streamNumericControlCompile(t, m, intr)
				got, _, e := runMemAmd64WithOptions(t, m, opts, nil, tc.arg)
				if e != nil || got != tc.want {
					t.Fatalf("intr%v got%d err%v want%d", intr, got, e, tc.want)
				}
				streamNumericControlSelected(t, opts)
			}
		})
	}
}
