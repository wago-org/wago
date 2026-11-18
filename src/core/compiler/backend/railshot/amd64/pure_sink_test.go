//go:build amd64

package amd64

// Reconstructed after workspace loss on live 752e12f7. These are new fixtures,
// not recovered source or evidence of the earlier experiment's test results.
import (
	"bytes"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"reflect"
	"testing"
)

func pureSinkFixture(typ machineType, state locState) *fn {
	return &fn{a: &encoder.Asm{}, s: newStack(), stats: &CodegenStats{}, policy: currentCodegenPolicy(),
		localType: []machineType{typ, typ}, localSlot: []uint32{0, 1},
		locals:       []localDef{{typ: typ, reg: R12, state: state}, {typ: typ, reg: R13, state: state}},
		pinnedLocals: []int{0, 1}, nLocals: 2, usesCalls: true}
}

func pureSinkLegacy(t *testing.T, f *fn, body []byte) *wasm.Reader {
	t.Helper()
	f.pushPlainLocalGet(0)
	r := wasm.NewReader(body)
	for r.Offset() < len(body)-1 {
		f.wasmPC = uint32(r.Offset())
		op, err := r.Byte()
		if err != nil {
			t.Fatal(err)
		}
		if err = f.emitPlain(r, op); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func pureSinkCompare(t *testing.T, old, fast *fn, q, r *wasm.Reader, tee bool) {
	t.Helper()
	if !bytes.Equal(old.a.B, fast.a.B) {
		t.Fatalf("native old=%x fast=%x", old.a.B, fast.a.B)
	}
	if q.Offset() != r.Offset() || old.wasmPC != fast.wasmPC || old.localWritten != fast.localWritten ||
		!reflect.DeepEqual(old.locals, fast.locals) || old.pinned != fast.pinned || old.reserved != fast.reserved ||
		old.regUser != fast.regUser || old.boundsCerts != fast.boundsCerts || old.boundsCertSources != fast.boundsCertSources ||
		old.gcResolved != fast.gcResolved {
		t.Fatal("compiler state differs")
	}
	depth := uint32(0)
	if tee {
		depth = 1
	}
	if old.s.logicalDepth != depth || fast.s.logicalDepth != depth || old.s.pendingEffects != fast.s.pendingEffects ||
		old.s.hasGCRoots != fast.s.hasGCRoots || old.s.pendingPrefixLoads != fast.s.pendingPrefixLoads ||
		old.s.canonicalSlots != fast.s.canonicalSlots {
		t.Fatal("logical/effect state differs")
	}
	if tee {
		a, b := old.s.back(), fast.s.back()
		if a == nil || b == nil || a.st != b.st || !a.st.hasLogicalRoot() || !b.st.hasLogicalRoot() ||
			a.prev != old.s.head || b.prev != fast.s.head || a.next != old.s.head || b.next != fast.s.head {
			t.Fatal("tee root differs")
		}
	} else if old.s.back() != nil || fast.s.back() != nil || old.s.head.next != old.s.head || fast.s.head.next != fast.s.head {
		t.Fatal("residual operand")
	}
}

func TestPureSinkStateParity(t *testing.T) {
	if profileEnabled || regallocCheckEnabled {
		t.Skip("explicit legacy-only diagnostic build")
	}
	for _, compact := range []bool{false, true} {
		for _, dst := range []byte{0, 1} {
			for _, tee := range []bool{false, true} {
				for _, typ := range []machineType{mtI32, mtI64} {
					for _, state := range []locState{lsReg, lsStackReg} {
						for _, op := range []byte{0x6a, 0x6b, 0x71, 0x72, 0x73} {
							for _, c := range []byte{1, 17, 0x7e} {
								t.Run(fmt.Sprintf("%v/%d/%v/%d/%d/%x/%x", compact, dst, tee, typ, state, op, c), func(t *testing.T) {
									co, ao, set := byte(0x41), op, byte(0x21)
									if typ == mtI64 {
										co = 0x42
										ao += 0x12
									}
									if tee {
										set = 0x22
									}
									body := []byte{co, c, ao, set, dst, 0x01}
									old, fast := pureSinkFixture(typ, state), pureSinkFixture(typ, state)
									for _, f := range []*fn{old, fast} {
										f.policy.CompactNative = compact
										f.boundsCerts[0] = boundsCert{kind: 1, idx: 0, extent: 32}
										f.boundsCerts[1] = boundsCert{kind: 1, idx: 1, extent: 64}
										f.boundsCertSources = boundsCertSourceBit(1, 0) | boundsCertSourceBit(1, 1)
									}
									r := wasm.NewReader(body)
									if !fast.tryPureLocalSink(r, 0) {
										t.Fatal("expected actual selection")
									}
									q := pureSinkLegacy(t, old, body)
									pureSinkCompare(t, old, fast, q, r, tee)
									if diagnosticsEnabled && fast.stats.Peephole["pure-local-sink"] != 1 {
										t.Fatal("missing selection counter")
									}
								})
							}
						}
					}
				}
			}
		}
	}
}

func TestPureSinkRejectsWithoutMutation(t *testing.T) {
	for i, body := range [][]byte{
		{0x41, 0, 0x6a, 0x21, 0}, {0x41, 0x7f, 0x71, 0x21, 0},
		{0x41, 3, 0x6d, 0x21, 0},          // trapping division
		{0x41, 3, 0x6a, 0x21, 2},          // invalid destination
		{0x41, 3, 0x6a, 0x21, 0, 0x20, 0}, // preserve set/get-to-tee priority
		{0x41, 0x80},                      // truncated immediate
	} {
		f := pureSinkFixture(mtI32, lsReg)
		before := *f
		stack := *f.s
		r := wasm.NewReader(body)
		if f.tryPureLocalSink(r, 0) {
			t.Fatalf("%d unexpectedly selected", i)
		}
		if r.Offset() != 0 || !reflect.DeepEqual(before, *f) || !reflect.DeepEqual(stack, *f.s) || f.a.Len() != 0 {
			t.Fatalf("%d mutated on rejection", i)
		}
	}
}

func TestPureSinkStateGuards(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*fn)
	}{
		{"gc-resolved", func(f *fn) { f.gcResolved.valid = true; f.gcResolved.local = 0 }},
		{"inline-depth", func(f *fn) { f.inlineDepth = 1 }},
		{"spilled-input", func(f *fn) { f.locals[0].state = lsMem }},
		{"lazy-zero", func(f *fn) { f.locals[0].state = lsConstZero }},
		{"pending-trap", func(f *fn) { f.s.pushIntegerConstant(mtI32, 7); f.s.pendingEffects |= pendingTrap }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := pureSinkFixture(mtI32, lsReg)
			tc.change(f)
			before := *f
			stack := *f.s
			r := wasm.NewReader([]byte{0x41, 3, 0x6a, 0x21, 0})
			if f.tryPureLocalSink(r, 0) {
				t.Fatal("guard accepted")
			}
			if r.Offset() != 0 || !reflect.DeepEqual(before, *f) || !reflect.DeepEqual(stack, *f.s) || f.a.Len() != 0 {
				t.Fatal("guard mutated state")
			}
		})
	}
}

func TestPureSinkTeeByteSwapPriority(t *testing.T) {
	if profileEnabled || regallocCheckEnabled {
		t.Skip("explicit legacy-only diagnostic build")
	}
	saved := byteSwapCoverEnabled
	byteSwapCoverEnabled = true
	defer func() { byteSwapCoverEnabled = saved }()
	for _, dst := range []byte{0, 1} {
		body := []byte{0x41, 17, 0x6a, 0x22, dst,
			0x41, 0xff, 0x81, 0xfc, 0x07, 0x71, 0x41, 8, 0x78, 0x20, dst,
			0x41, 24, 0x78, 0x41, 0xff, 0x81, 0xfc, 0x07, 0x71, 0x72, 0x01}
		old, fast := pureSinkFixture(mtI32, lsReg), pureSinkFixture(mtI32, lsReg)
		r := wasm.NewReader(body)
		if !fast.tryPureLocalSink(r, 0) {
			t.Fatal("expected actual selection")
		}
		q := pureSinkLegacy(t, old, body)
		// Register owner pointers differ between independent fixtures; compare the
		// result storage and owner identity relative to each local stack instead.
		if r.Offset() != q.Offset() || !bytes.Equal(old.a.B, fast.a.B) {
			t.Fatal("post-tee selector priority differs")
		}
		a, b := old.s.back(), fast.s.back()
		if a == nil || b == nil || a.st != b.st || old.s.pendingEffects != fast.s.pendingEffects ||
			old.s.logicalDepth != fast.s.logicalDepth || !reflect.DeepEqual(old.locals, fast.locals) {
			t.Fatal("post-tee result differs")
		}
		if diagnosticsEnabled && (old.stats.Peephole["i32-bswap-tee"] != 1 || fast.stats.Peephole["i32-bswap-tee"] != 1 ||
			fast.stats.Peephole["pure-local-sink"] != 1) {
			t.Fatal("missing positive sink/byte-swap cover")
		}
	}
}
