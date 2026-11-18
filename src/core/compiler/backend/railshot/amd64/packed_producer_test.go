//go:build amd64

package amd64

import (
	"bytes"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func packedProducerTestEnabled(t *testing.T) {
	packedTestEnabled(t)
	old := packedProducerEnabled
	packedProducerEnabled = true
	t.Cleanup(func() { packedProducerEnabled = old })
}
func packedProducerTestFn(typ machineType) *fn {
	f := packedTestFn()
	f.localType = []machineType{typ, typ}
	f.localSlot = []uint32{0, 8}
	f.locals = []localDef{{typ: typ, reg: R12, state: lsReg}, {typ: typ, reg: R8, state: lsReg}}
	f.pinnedLocals = []int{0, 1}
	f.pinnedLocalMask = maskOf(R12).union(maskOf(R8))
	return f
}
func packedProducerTestStorage(l packedProducerLeaf) storage {
	switch uint32(l.kind) {
	case terminalConstant:
		return storage{kind: stConst, typ: l.typ, cval: l.value}
	case terminalLocalRef:
		return storage{kind: stLocalRef, typ: l.typ, idx: l.index}
	case terminalLocalReg:
		return storage{kind: stLocalReg, typ: l.typ, idx: l.index, reg: l.reg}
	default:
		panic("bad producer fixture kind")
	}
}
func TestPackedProducerMatchesLegacy(t *testing.T) {
	packedProducerTestEnabled(t)
	for _, typ := range []machineType{mtI32, mtI64} {
		for oi, op := range []wOp{opAdd, opSub, opAnd, opOr, opXor} {
			for _, shape := range []string{"local-local", "local-constant", "constant-local"} {
				t.Run(fmt.Sprintf("type%d-op%d-%s", typ, op, shape), func(t *testing.T) {
					a, b := packedProducerTestFn(typ), packedProducerTestFn(typ)
					ao := a.s.pushValue(storage{kind: stConst, typ: typ, cval: 99})
					bo := b.s.pushValue(storage{kind: stConst, typ: typ, cval: 99})
					a.s.pendingPrefix = ao
					b.s.pendingPrefix = bo
					left, ok := b.packedProducerLocal(0)
					if !ok {
						t.Fatal("local denied")
					}
					right, ok := b.packedProducerLocal(1)
					if !ok {
						t.Fatal("local denied")
					}
					constant := packedProducerLeaf{kind: uint8(terminalConstant), typ: typ, value: 7}
					second := []byte{0x20, 1}
					if shape == "local-constant" {
						right = constant
						cop := byte(0x41)
						if typ == mtI64 {
							cop = 0x42
						}
						second = []byte{cop, 7}
					}
					if shape == "constant-local" {
						left = constant
					}
					binary := []byte{0x6a, 0x6b, 0x71, 0x72, 0x73}[oi]
					if typ == mtI64 {
						binary = []byte{0x7c, 0x7d, 0x83, 0x84, 0x85}[oi]
					}
					source := append(append([]byte{}, second...), binary, 0x21, 0)
					r := wasm.NewReader(source)
					b.tracePCBase = 100
					if !b.tryPackedProducer(r, left) {
						t.Fatal("producer rejected supported pair")
					}
					if r.Offset() != len(second)+1 || b.wasmPC != 100+uint32(len(second)) {
						t.Fatal("source cursor/origin mismatch")
					}
					a.s.pushValue(packedProducerTestStorage(left))
					a.s.pushValue(packedProducerTestStorage(right))
					a.pushBinOpWithPacking(op, typ, true)
					an, bn := a.s.back(), b.s.back()
					if !an.isPackedTerminal() || !bn.isPackedTerminal() {
						t.Fatal("active path not exercised")
					}
					if an.st != bn.st || a.s.logicalDepth != b.s.logicalDepth || a.s.pendingEffects != b.s.pendingEffects || b.s.pendingPrefix != bo || bn.prev != bo || len(b.a.B) != 0 {
						t.Fatalf("packed state mismatch %+v/%+v", an.st, bn.st)
					}
					ar, br := a.condensePackedTerminal(an, regNone), b.condensePackedTerminal(bn, regNone)
					if ar != br || !bytes.Equal(a.a.B, b.a.B) || an.st != bn.st || b.s.packedCount != 0 {
						t.Fatal("materialized native/storage differs")
					}
				})
			}
		}
	}
}
func TestPackedProducerRejectsTransactionally(t *testing.T) {
	packedProducerTestEnabled(t)
	cases := []struct {
		name   string
		source []byte
		edit   func(*fn)
	}{
		{"truncated-local", []byte{0x20, 0x80}, nil},
		{"truncated-constant", []byte{0x41, 0x80}, nil},
		{"local-range", []byte{0x20, 9, 0x6a, 0x21, 0}, nil},
		{"wrong-constant-width", []byte{0x42, 7, 0x6a, 0x21, 0}, nil},
		{"unsupported-op", []byte{0x20, 1, 0x6c, 0x21, 0}, nil},
		{"no-successor", []byte{0x20, 1, 0x6a}, nil},
		{"expanding-successor", []byte{0x20, 1, 0x6a, 0x04, 0x40}, nil},
		{"rhs-identity", []byte{0x41, 0, 0x6a, 0x21, 0}, nil},
		{"same-local-fold", []byte{0x20, 0, 0x6b, 0x21, 0}, nil},
		{"inline", []byte{0x20, 1, 0x6a, 0x21, 0}, func(f *fn) { f.inlineDepth = 1 }},
		{"depth-overflow", []byte{0x20, 1, 0x6a, 0x21, 0}, func(f *fn) { f.s.logicalDepth = ^uint32(0) - 1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := packedProducerTestFn(mtI32)
			first, ok := f.packedProducerLocal(0)
			if !ok {
				t.Fatal("fixture local")
			}
			outer := f.s.pushValue(storage{kind: stConst, typ: mtI32, cval: 99})
			f.s.pendingPrefix = outer
			if c.edit != nil {
				c.edit(f)
			}
			r := wasm.NewReader(c.source)
			before := *outer
			depth, effects, pc := f.s.logicalDepth, f.s.pendingEffects, f.wasmPC
			if f.tryPackedProducer(r, first) {
				t.Fatal("invalid pair accepted")
			}
			if r.Offset() != 0 || *outer != before || f.s.back() != outer || f.s.logicalDepth != depth || f.s.pendingEffects != effects || f.wasmPC != pc || f.s.packedCount != 0 || len(f.a.B) != 0 {
				t.Fatal("failed producer mutated state")
			}
		})
	}
}

// Replay helpers may hand emitPlain a sliced reader with an unrelated wasmPC.
// Only the explicit body-loop entry owns source offsets for macro lookahead.
func TestPackedProducerReplayReaderProvenance(t *testing.T) {
	packedProducerTestEnabled(t)
	for _, typ := range []machineType{mtI32, mtI64} {
		for _, firstLocal := range []bool{true, false} {
			for _, allow := range []bool{false, true} {
				t.Run(fmt.Sprintf("type%d-local%v-body%v", typ, firstLocal, allow), func(t *testing.T) {
					f := packedProducerTestFn(typ)
					f.wasmPC = 4000
					f.tracePCBase = 100
					op, immediate, binary := byte(0x20), byte(0), byte(0x6a)
					if typ == mtI64 {
						binary = 0x7c
					}
					if !firstLocal {
						op = 0x41
						immediate = 7
						if typ == mtI64 {
							op = 0x42
						}
					}
					// The first opcode was consumed by the caller; reader starts at its immediate.
					r := wasm.NewReader([]byte{immediate, 0x20, 1, binary, 0x21, 0})
					var err error
					if allow {
						err = f.emitPlainWithProducer(r, op, true)
					} else {
						err = f.emitPlain(r, op)
					}
					if err != nil {
						t.Fatal(err)
					}
					if f.s.logicalDepth != 1 {
						t.Fatal("wrong logical result count")
					}
					if allow {
						if r.Offset() != 4 || f.wasmPC != 103 || !f.s.back().isPackedTerminal() || f.s.packedCount != 1 {
							t.Fatalf("body source origin/cursor incorrect pc=%d offset=%d", f.wasmPC, r.Offset())
						}
					} else {
						if r.Offset() != 1 || f.wasmPC != 4000 || f.s.back().isPackedTerminal() || f.s.packedCount != 0 {
							t.Fatalf("replay consumed or rewrote caller provenance pc=%d offset=%d", f.wasmPC, r.Offset())
						}
						if firstLocal {
							if f.s.back().st.idx != 0 {
								t.Fatal("wrong local leaf")
							}
						} else if f.s.back().st.kind != stConst || f.s.back().st.cval != 7 {
							t.Fatal("wrong constant leaf")
						}
					}
				})
			}
		}
	}
}
