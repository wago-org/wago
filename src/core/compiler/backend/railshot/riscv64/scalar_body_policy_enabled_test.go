//go:build linux && !tinygo && wago_regalloccheck

package riscv64

import (
	"encoding/binary"
	"slices"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func scalarPolicyReturn(a *enc.Asm, prefix int) {
	switch prefix {
	case 1:
		a.Addi(enc.X30, enc.X5, 0)
	case 2:
		a.ZeroExtend32(enc.X30, enc.X5)
	}
	a.Ld(enc.X31, enc.SP, 8)
	a.Sd(enc.X30, enc.X31, 0)
	a.Addi(enc.X10, enc.X30, 0)
}
func scalarPolicyAppend(t *testing.T, r *scalarBodyReceipt, code []byte, emit func(*enc.Asm)) ([]byte, int) {
	t.Helper()
	return scalarReceiptAppend(t, r, code, func(a *enc.Asm) *scalarReceiptEnvelope { emit(a); return nil })
}
func scalarPolicyCheckFailure(t *testing.T, r *scalarBodyReceipt, code []byte, frame scalarBodyFrame, id int, verdict regalloccheck.Verdict) {
	t.Helper()
	facts, ok := r.CheckBodyPolicy(code, frame, id)
	if ok || r.failure.Verdict != verdict || facts != (scalarBodyPolicyFacts{}) {
		t.Fatal("unexpected policy result", facts, ok, r.failure)
	}
}

func TestScalarBodyPolicyFrameAndSerializedReturn(t *testing.T) {
	for _, prefix := range []int{0, 1, 2} {
		r := newScalarBodyReceipt(scalarReceiptLimits{})
		code, _ := scalarPolicyAppend(t, r, nil, func(a *enc.Asm) { a.Lwu(enc.X5, enc.SP, 16) })
		code, _ = scalarPolicyAppend(t, r, code, func(a *enc.Asm) { a.Sw(enc.X5, enc.SP, 24) })
		code, _ = scalarPolicyAppend(t, r, code, func(a *enc.Asm) {
			a.Constant64(enc.X31, 4096)
			a.Add(enc.X31, enc.SP, enc.X31)
			a.Sd(enc.X5, enc.X31, 0)
		})
		code, _ = scalarPolicyAppend(t, r, code, func(a *enc.Asm) {
			a.Constant64(enc.X31, 4096)
			a.Add(enc.X31, enc.SP, enc.X31)
			a.Ld(enc.X5, enc.X31, 0)
		})
		code, id := scalarPolicyAppend(t, r, code, func(a *enc.Asm) { scalarPolicyReturn(a, prefix) })
		code, _ = scalarPolicyAppend(t, r, code, func(*enc.Asm) {}) // empty terminal scope
		frame := scalarBodyFrame{Bytes: 4112, Locals: 511, Spills: 1}
		facts, ok := r.CheckBodyPolicy(code, frame, id)
		want := regalloccheck.Effect{Kind: regalloccheck.Copy, Dst: regalloccheck.Register(regalloccheck.ReturnArea, 0), Src: scalarWordGP(30), Size: 8}
		if !ok || facts.FrameLoads != 2 || facts.FrameStores != 2 || facts.ReturnSpan != id || facts.ReturnEffect != want {
			t.Fatal(prefix, facts, r.failure)
		}
		// Repeat against an independently copied final body; no new retained arrays.
		dataCap, spanCap, storage := cap(r.data), cap(r.spans), r.storage
		if _, ok = r.CheckBodyPolicy(slices.Clone(code), frame, id); !ok || cap(r.data) != dataCap || cap(r.spans) != spanCap || r.storage != storage {
			t.Fatal("recheck", r.failure)
		}
	}
}

func TestScalarBodyPolicyEveryStoreAndSPWrite(t *testing.T) {
	cases := []struct {
		name    string
		emit    func(*enc.Asm)
		verdict regalloccheck.Verdict
	}{
		{"sp move restore", func(a *enc.Asm) { a.Addi(enc.SP, enc.SP, -16); a.Addi(enc.SP, enc.SP, 16) }, regalloccheck.Rejected},
		{"sp nop", func(a *enc.Asm) { a.Addi(enc.SP, enc.SP, 0) }, regalloccheck.Rejected},
		{"load to sp", func(a *enc.Asm) { a.Ld(enc.SP, enc.SP, 16) }, regalloccheck.Rejected},
		{"zero header store", func(a *enc.Asm) { a.Sd(enc.Zero, enc.SP, 8) }, regalloccheck.Rejected},
		{"negative overlap", func(a *enc.Asm) { a.Sd(enc.X5, enc.SP, -1) }, regalloccheck.Rejected},
		{"word overlap", func(a *enc.Asm) { a.Sw(enc.X5, enc.SP, 15) }, regalloccheck.Rejected},
		{"byte header", func(a *enc.Asm) { a.Sb(enc.Zero, enc.SP, 7) }, regalloccheck.Rejected},
		{"header load zero", func(a *enc.Asm) { a.Ld(enc.Zero, enc.SP, 8) }, regalloccheck.Rejected},
		{"negative nonheader", func(a *enc.Asm) { a.Sd(enc.X5, enc.SP, -16) }, regalloccheck.Rejected},
		{"dynamic header", func(a *enc.Asm) {
			a.Constant64(enc.X31, 8)
			a.Add(enc.X31, enc.SP, enc.X31)
			a.Sd(enc.Zero, enc.X31, 0)
		}, regalloccheck.Rejected},
		{"dynamic cancelled offset", func(a *enc.Asm) {
			a.Constant64(enc.X31, 2048)
			a.Add(enc.X31, enc.SP, enc.X31)
			a.Sw(enc.X5, enc.X31, -2040)
		}, regalloccheck.Rejected},
		{"unknown store base", func(a *enc.Asm) { a.Sd(enc.X5, enc.X6, 0) }, regalloccheck.Inconclusive},
		{"unknown load zero", func(a *enc.Asm) { a.Ld(enc.Zero, enc.X6, 0) }, regalloccheck.Inconclusive},
		{"unknown dynamic base", func(a *enc.Asm) { a.Constant64(enc.X31, 16); a.Add(enc.X31, enc.X6, enc.X31); a.Sd(enc.X5, enc.X31, 0) }, regalloccheck.Inconclusive},
		{"extra memory", func(a *enc.Asm) { a.Ld(enc.X5, enc.SP, 16); a.Sd(enc.X5, enc.SP, 24) }, regalloccheck.Inconclusive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newScalarBodyReceipt(scalarReceiptLimits{})
			code, _ := scalarPolicyAppend(t, r, nil, tc.emit)
			code, id := scalarPolicyAppend(t, r, code, func(a *enc.Asm) { scalarPolicyReturn(a, 1) })
			scalarPolicyCheckFailure(t, r, code, scalarBodyFrame{Bytes: 32, Locals: 2}, id, tc.verdict)
		})
	}
}

func TestScalarBodyPolicyOwnedSlotBounds(t *testing.T) {
	cases := []struct {
		name   string
		offset int32
		size   int
		frame  scalarBodyFrame
		valid  bool
	}{
		{"first", 16, 8, scalarBodyFrame{Bytes: 32, Locals: 1}, true},
		{"padding", 24, 8, scalarBodyFrame{Bytes: 32, Locals: 1}, false},
		{"cell crossing", 20, 8, scalarBodyFrame{Bytes: 32, Locals: 2}, false},
		{"high word", 20, 4, scalarBodyFrame{Bytes: 32, Locals: 1}, true},
		{"byte end", 23, 1, scalarBodyFrame{Bytes: 32, Locals: 1}, true},
		{"last spill", 32, 8, scalarBodyFrame{Bytes: 48, Locals: 1, Spills: 2}, true},
		{"past spill", 40, 8, scalarBodyFrame{Bytes: 48, Locals: 1, Spills: 2}, false},
		{"large last", MaxFrameBytes - 8, 8, scalarBodyFrame{Bytes: MaxFrameBytes, Locals: (MaxFrameBytes - 16) / 8}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newScalarBodyReceipt(scalarReceiptLimits{})
			code, _ := scalarPolicyAppend(t, r, nil, func(a *enc.Asm) {
				base, off := enc.SP, tc.offset
				if off > 2047 {
					a.Constant64(enc.X31, int64(off))
					a.Add(enc.X31, enc.SP, enc.X31)
					base, off = enc.X31, 0
				}
				switch tc.size {
				case 1:
					a.Sb(enc.X5, base, off)
				case 4:
					a.Sw(enc.X5, base, off)
				case 8:
					a.Sd(enc.X5, base, off)
				}
			})
			code, id := scalarPolicyAppend(t, r, code, func(a *enc.Asm) { scalarPolicyReturn(a, 2) })
			facts, ok := r.CheckBodyPolicy(code, tc.frame, id)
			if ok != tc.valid || ok && facts.FrameStores != 1 || !ok && r.failure.Verdict != regalloccheck.Rejected {
				t.Fatal(facts, ok, r.failure)
			}
		})
	}
}

func TestScalarBodyPolicyReturnRecipeMutations(t *testing.T) {
	cases := []struct {
		name string
		emit func(*enc.Asm)
	}{
		{"pointer offset", func(a *enc.Asm) { a.Ld(enc.X31, enc.SP, 16); a.Sd(enc.X30, enc.X31, 0); a.Addi(enc.X10, enc.X30, 0) }},
		{"pointer base", func(a *enc.Asm) { a.Ld(enc.X31, enc.X5, 8); a.Sd(enc.X30, enc.X31, 0); a.Addi(enc.X10, enc.X30, 0) }},
		{"pointer destination", func(a *enc.Asm) { a.Ld(enc.X29, enc.SP, 8); a.Sd(enc.X30, enc.X31, 0); a.Addi(enc.X10, enc.X30, 0) }},
		{"store source", func(a *enc.Asm) { a.Ld(enc.X31, enc.SP, 8); a.Sd(enc.X5, enc.X31, 0); a.Addi(enc.X10, enc.X30, 0) }},
		{"store zero", func(a *enc.Asm) { a.Ld(enc.X31, enc.SP, 8); a.Sd(enc.Zero, enc.X31, 0); a.Addi(enc.X10, enc.X30, 0) }},
		{"store width", func(a *enc.Asm) { a.Ld(enc.X31, enc.SP, 8); a.Sw(enc.X30, enc.X31, 0); a.Addi(enc.X10, enc.X30, 0) }},
		{"store offset", func(a *enc.Asm) { a.Ld(enc.X31, enc.SP, 8); a.Sd(enc.X30, enc.X31, 8); a.Addi(enc.X10, enc.X30, 0) }},
		{"store missing", func(a *enc.Asm) { a.Ld(enc.X31, enc.SP, 8); a.Nop(); a.Addi(enc.X10, enc.X30, 0) }},
		{"store reordered", func(a *enc.Asm) { a.Sd(enc.X30, enc.X31, 0); a.Ld(enc.X31, enc.SP, 8); a.Addi(enc.X10, enc.X30, 0) }},
		{"pointer clobber", func(a *enc.Asm) {
			a.Ld(enc.X31, enc.SP, 8)
			a.Addi(enc.X31, enc.X5, 0)
			a.Sd(enc.X30, enc.X31, 0)
			a.Addi(enc.X10, enc.X30, 0)
		}},
		{"source clobber", func(a *enc.Asm) {
			a.Ld(enc.X31, enc.SP, 8)
			a.Sd(enc.X30, enc.X31, 0)
			a.Addi(enc.X30, enc.Zero, 0)
			a.Addi(enc.X10, enc.X30, 0)
		}},
		{"prefix wrong dest", func(a *enc.Asm) { a.Addi(enc.X29, enc.X5, 0); scalarPolicyReturn(a, 0) }},
		{"prefix shift", func(a *enc.Asm) { a.Slli(enc.X30, enc.X5, 31); a.Srli(enc.X30, enc.X30, 32); scalarPolicyReturn(a, 0) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newScalarBodyReceipt(scalarReceiptLimits{})
			code, id := scalarPolicyAppend(t, r, nil, tc.emit)
			scalarPolicyCheckFailure(t, r, code, scalarBodyFrame{Bytes: 16}, id, regalloccheck.Rejected)
		})
	}
}

func TestScalarBodyPolicyDeadStoresAndInteriorTargets(t *testing.T) {
	r := newScalarBodyReceipt(scalarReceiptLimits{})
	code, branch := scalarReceiptAppend(t, r, nil, func(a *enc.Asm) *scalarReceiptEnvelope {
		start := a.Len()
		site := a.JumpLong(false)
		return &scalarReceiptEnvelope{Start: start, Site: site, End: a.Len()}
	})
	code, _ = scalarPolicyAppend(t, r, code, func(a *enc.Asm) { a.Sb(enc.Zero, enc.SP, 15) })
	returnStart := len(code)
	code, id := scalarPolicyAppend(t, r, code, func(a *enc.Asm) { scalarPolicyReturn(a, 2) })
	scalarReceiptPatch(t, r, code, branch, returnStart)
	scalarPolicyCheckFailure(t, r, code, scalarBodyFrame{Bytes: 16}, id, regalloccheck.Rejected)
	// A valid-looking Return cannot lend pointer lineage to a branch into its
	// middle. The owned receipt's operation-boundary policy rejects it first.
	for _, delta := range []int{4, 8, 12, 16} {
		r := newScalarBodyReceipt(scalarReceiptLimits{})
		code, branch := scalarReceiptAppend(t, r, nil, func(a *enc.Asm) *scalarReceiptEnvelope {
			start := a.Len()
			site := a.JumpLong(false)
			return &scalarReceiptEnvelope{Start: start, Site: site, End: a.Len()}
		})
		start := len(code)
		code, _ = scalarPolicyAppend(t, r, code, func(a *enc.Asm) { scalarPolicyReturn(a, 2) })
		if _, ok := r.BeginRewrite(code, branch, start+delta); ok {
			t.Fatal("Return interior accepted", delta)
		}
	}
}

func TestScalarBodyPolicyFinalBytesLayoutAndBudgets(t *testing.T) {
	makeBody := func(limits scalarReceiptLimits) (*scalarBodyReceipt, []byte, int) {
		r := newScalarBodyReceipt(limits)
		code, id := scalarPolicyAppend(t, r, nil, func(a *enc.Asm) { scalarPolicyReturn(a, 1) })
		return r, code, id
	}
	for _, frame := range []scalarBodyFrame{{Bytes: 0}, {Bytes: 32}, {Bytes: 16, Locals: 1}, {Bytes: 16, Spills: -1}, {Bytes: 16, Locals: MaxFrameBytes}, {Bytes: MaxFrameBytes + 16}} {
		r, code, id := makeBody(scalarReceiptLimits{})
		if facts, ok := r.CheckBodyPolicy(code, frame, id); ok || facts != (scalarBodyPolicyFacts{}) {
			t.Fatal(frame, facts)
		}
	}
	for _, id := range []int{-1, 1} {
		r, code, _ := makeBody(scalarReceiptLimits{})
		scalarPolicyCheckFailure(t, r, code, scalarBodyFrame{Bytes: 16}, id, regalloccheck.Inconclusive)
	}
	r, code, id := makeBody(scalarReceiptLimits{})
	code, _ = scalarPolicyAppend(t, r, code, func(a *enc.Asm) { a.Nop() })
	scalarPolicyCheckFailure(t, r, code, scalarBodyFrame{Bytes: 16}, id, regalloccheck.Rejected)
	r, code, id = makeBody(scalarReceiptLimits{})
	if _, ok := r.CheckBodyPolicy(code, scalarBodyFrame{Bytes: 16}, id); !ok {
		t.Fatal(r.failure)
	}
	// Different ADDI source preserves the captured raw trace shape, but exact
	// receipt equality rejects post-capture mutation before reusing sink facts.
	word := binary.LittleEndian.Uint32(code)
	binary.LittleEndian.PutUint32(code, word^1<<15)
	scalarPolicyCheckFailure(t, r, code, scalarBodyFrame{Bytes: 16}, id, regalloccheck.Rejected)
	r, code, id = makeBody(scalarReceiptLimits{})
	// Charge final equality first, then permit one fewer work unit than policy.
	if !r.Seal(code) {
		t.Fatal(r.failure)
	}
	remainingSeal := 1 + len(code)/4 + len(r.spans)*4 // an upper bound on this no-envelope seal
	r.limits.Work = r.work + remainingSeal            // insufficient for policy scan
	scalarPolicyCheckFailure(t, r, code, scalarBodyFrame{Bytes: 16}, id, regalloccheck.Inconclusive)
	if r.failure.Reason != regalloccheck.ResourceLimit {
		t.Fatal(r.failure)
	}
}

func TestScalarBodyPolicyNoRetainedAllocationOrCopiedAuthority(t *testing.T) {
	r := newScalarBodyReceipt(scalarReceiptLimits{})
	code, id := scalarPolicyAppend(t, r, nil, func(a *enc.Asm) { scalarPolicyReturn(a, 2) })
	var failed bool
	allocations := testing.AllocsPerRun(10, func() {
		_, ok := r.CheckBodyPolicy(code, scalarBodyFrame{Bytes: 16}, id)
		failed = failed || !ok
	})
	if failed || allocations != 0 {
		t.Fatal("bounded policy scan allocated or failed", allocations, r.failure)
	}
	copied := *r
	scalarPolicyCheckFailure(t, &copied, code, scalarBodyFrame{Bytes: 16}, id, regalloccheck.Inconclusive)
	if _, ok := r.CheckBodyPolicy(code, scalarBodyFrame{Bytes: 16}, id); !ok {
		t.Fatal("copy failure invalidated original", r.failure)
	}
}
