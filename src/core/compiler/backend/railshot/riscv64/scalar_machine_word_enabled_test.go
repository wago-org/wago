//go:build linux && !tinygo && wago_regalloccheck

package riscv64

import (
	"encoding/binary"
	"math"
	"slices"
	"testing"

	"github.com/wago-org/wago/internal/regalloccheck"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
)

func scalarWordCapture(t *testing.T, prefix int, emit func(*enc.Asm)) ([]byte, []scalarWordObservation) {
	t.Helper()
	a := enc.Asm{B: make([]byte, prefix)}
	var observations []scalarWordObservation
	previousWrites := a.ObserveGPWrites(func(mask uint32) {
		observations = append(observations, scalarWordObservation{a.Len(), scalarWordEvent{Write: true, Mask: mask}})
	})
	defer func() { a.ObserveGPWrites(previousWrites) }()
	previousEffects := a.ObserveRegalloc(func(effect regalloccheck.Effect) {
		observations = append(observations, scalarWordObservation{a.Len(), scalarWordEvent{Effect: effect}})
	})
	defer func() { a.ObserveRegalloc(previousEffects) }()
	emit(&a)
	if a.Err() != nil {
		t.Fatal(a.Err())
	}
	return a.B, observations
}

func scalarWordAuditOK(t *testing.T, code []byte, start int, events []scalarWordObservation) scalarWordAuditSummary {
	t.Helper()
	summary, status, ok := auditScalarWordTrace(code, start, len(code), events, scalarWordAuditLimits{})
	if !ok || status.Reason != regalloccheck.NoFailure {
		t.Fatal("word/trace mismatch", summary, status)
	}
	return summary
}

func TestScalarMachineWordActualEncoderTraces(t *testing.T) {
	// Every case is emitted through the real encoder, while expected trace
	// reconstruction reads the independent final word fields only.
	for _, dst := range []enc.Reg{enc.Zero, enc.SP, enc.X5, enc.X31} {
		for _, src := range []enc.Reg{enc.Zero, enc.SP, enc.X5, enc.X31} {
			for _, emit := range []func(*enc.Asm){
				func(a *enc.Asm) { a.Addi(dst, src, 0) },
				func(a *enc.Asm) { a.Addi(dst, src, -2048) },
				func(a *enc.Asm) { a.Addiw(dst, src, 0) },
				func(a *enc.Asm) { a.Xori(dst, src, -1) },
				func(a *enc.Asm) { a.Sltiu(dst, src, 1) },
				func(a *enc.Asm) { a.Slli(dst, src, 32) },
				func(a *enc.Asm) { a.Srli(dst, src, 32) },
				func(a *enc.Asm) { a.Srai(dst, src, 32) },
				func(a *enc.Asm) { a.Slli(dst, src, 63) },
				func(a *enc.Asm) { a.Srliw(dst, src, 31) },
				func(a *enc.Asm) { a.Sraiw(dst, src, 31) },
				func(a *enc.Asm) { a.Ld(dst, src, 8) },
				func(a *enc.Asm) { a.Lw(dst, src, -8) },
				func(a *enc.Asm) { a.Lwu(dst, src, 2047) },
				func(a *enc.Asm) { a.Lbu(dst, src, -2048) },
				func(a *enc.Asm) { a.Sd(dst, src, 8) },
				func(a *enc.Asm) { a.Sw(dst, src, -8) },
				func(a *enc.Asm) { a.Sb(dst, src, -2048) },
				func(a *enc.Asm) { a.Add(dst, src, enc.X6) },
				func(a *enc.Asm) { a.Sub(dst, src, enc.X6) },
				func(a *enc.Asm) { a.Mul(dst, src, enc.X6) },
				func(a *enc.Asm) { a.Divu(dst, src, enc.X6) },
				func(a *enc.Asm) { a.Sra(dst, src, enc.X6) },
				func(a *enc.Asm) { a.Sltu(dst, src, enc.X6) },
				func(a *enc.Asm) { a.Addw(dst, src, enc.X6) },
				func(a *enc.Asm) { a.Subw(dst, src, enc.X6) },
				func(a *enc.Asm) { a.Remuw(dst, src, enc.X6) },
				func(a *enc.Asm) { a.Sraw(dst, src, enc.X6) },
				func(a *enc.Asm) { a.Lui(dst, 0xfffff) },
				func(a *enc.Asm) { a.Auipc(dst, 0x80000) },
				func(a *enc.Asm) { a.Jalr(dst, src, -4) },
				func(a *enc.Asm) { a.Jal(dst, -4096) },
			} {
				code, observations := scalarWordCapture(t, 0, emit)
				summary := scalarWordAuditOK(t, code, 0, observations)
				if summary.Words != 1 || summary.Events != len(observations) {
					t.Fatal(summary)
				}
			}
		}
	}
	for _, value := range []int64{0, -2048, 2047, 2048, -2049, 0x7fffffff, 0x80000000, math.MaxInt64, math.MinInt64} {
		code, observations := scalarWordCapture(t, 12, func(a *enc.Asm) {
			a.Constant64(enc.X5, value)
			a.Constant32(enc.X6, uint32(value))
			a.ZeroExtend32(enc.X5, enc.X5)
			a.Nop()
		})
		scalarWordAuditOK(t, code, 12, observations)
	}
	for _, condition := range []enc.Cond{enc.EQ, enc.NE, enc.LT, enc.GE, enc.LTU, enc.GEU} {
		for _, offset := range []int32{-4096, -4, 0, 4, 4092} {
			code, events := scalarWordCapture(t, 0, func(a *enc.Asm) { a.Branch(condition, enc.X5, enc.X6, offset) })
			d, ok := decodeScalarMachineWord(binary.LittleEndian.Uint32(code))
			if !ok || d.Control != scalarWordBranch || d.Offset != offset {
				t.Fatal(d, ok)
			}
			scalarWordAuditOK(t, code, 0, events)
		}
	}
}

func TestScalarMachineWordNoEventHazardsAreExplicit(t *testing.T) {
	for _, test := range []struct {
		word           uint32
		memory         scalarWordMemory
		control        scalarWordControl
		base, register uint8
		size           int
		offset         int32
	}{
		{0x00813003, scalarWordLoad, scalarWordNoControl, 2, 0, 8, 8},    // LD x0,8(sp)
		{0x01efb023, scalarWordStore, scalarWordNoControl, 31, 30, 8, 0}, // SD x30,0(x31)
		{0x00008067, scalarWordNoMemory, scalarWordIndirect, 1, 0, 0, 0}, // RET
	} {
		d, ok := decodeScalarMachineWord(test.word)
		if !ok || d.Count != 0 || d.Memory != test.memory || d.Control != test.control || d.Base != test.base || d.Register != test.register || d.Size != test.size || d.Offset != test.offset {
			t.Fatal("silent hazard lost", test.word, d, ok)
		}
	}
	d, ok := decodeScalarMachineWord(0x00013423) // SD x0,8(sp)
	if !ok || d.Memory != scalarWordStore || d.Offset != 8 || d.Count != 1 || d.Events[0].Effect.Kind != regalloccheck.Kill || d.Events[0].Effect.Dst != regalloccheck.Slot(8) {
		t.Fatal("zero header store lost", d, ok)
	}
	d, ok = decodeScalarMachineWord(0x00010113) // ADDI sp,sp,0
	if !ok || d.Writes != 1<<2 || d.Count != 2 || d.Events[0].Effect.Kind != regalloccheck.Copy || !d.Events[1].Write {
		t.Fatal("SP write lost", d, ok)
	}
	d, ok = decodeScalarMachineWord(0x000000ef) // JAL ra,0
	if !ok || d.Link != 1 || d.Writes != 1<<1 || d.Count != 2 || d.Events[0].Mask != ^uint32(1) || d.Events[1].Effect.Kind != regalloccheck.Call {
		t.Fatal("call mask confused with actual SP write", d, ok)
	}
	code, events := scalarWordCapture(t, 0, func(a *enc.Asm) {
		a.Ld(enc.X31, enc.SP, 8)
		a.Sd(enc.X30, enc.X31, 0)
		a.Addi(enc.X10, enc.X30, 0)
		a.Addi(enc.SP, enc.SP, 0)
		a.Jal(enc.RA, 0)
	})
	summary := scalarWordAuditOK(t, code, 0, events)
	if summary.Loads != 1 || summary.Stores != 1 || summary.Controls != 1 || summary.SPWrites != 1 {
		t.Fatal(summary)
	}
	// Trace consistency above is deliberately NOT a valid body/return proof:
	// it contains a forbidden SP write and a call, and the output base is unknown.
}

func TestScalarMachineWordMutationsAndTraceCoverage(t *testing.T) {
	code, events := scalarWordCapture(t, 0, func(a *enc.Asm) {
		a.Slli(enc.X5, enc.X6, 32)
		a.Ld(enc.X7, enc.SP, 16)
		a.Sd(enc.X5, enc.SP, 24)
	})
	for _, mutate := range []func([]byte){
		func(b []byte) { binary.LittleEndian.PutUint32(b, binary.LittleEndian.Uint32(b)^1<<7) },          // real destination
		func(b []byte) { binary.LittleEndian.PutUint32(b, binary.LittleEndian.Uint32(b)^1<<15) },         // real source
		func(b []byte) { binary.LittleEndian.PutUint32(b, binary.LittleEndian.Uint32(b)^1<<20) },         // shift31/33 is not Copy32
		func(b []byte) { binary.LittleEndian.PutUint32(b[4:], binary.LittleEndian.Uint32(b[4:])^8<<20) }, // real frame offset
		func(b []byte) { binary.LittleEndian.PutUint32(b[8:], binary.LittleEndian.Uint32(b[8:])^1<<20) }, // real store source
		func(b []byte) { binary.LittleEndian.PutUint32(b[8:], binary.LittleEndian.Uint32(b[8:])^1<<15) }, // store base no longer SP
	} {
		mutated := slices.Clone(code)
		mutate(mutated)
		_, status, ok := auditScalarWordTrace(mutated, 0, len(mutated), events, scalarWordAuditLimits{})
		if ok || status.Verdict != regalloccheck.Rejected {
			t.Fatal("stale events accepted", status)
		}
	}
	for _, mutate := range []func([]scalarWordObservation) []scalarWordObservation{
		func(e []scalarWordObservation) []scalarWordObservation { return e[1:] },
		func(e []scalarWordObservation) []scalarWordObservation { return append(e, e[0]) },
		func(e []scalarWordObservation) []scalarWordObservation { e[0], e[1] = e[1], e[0]; return e },
		func(e []scalarWordObservation) []scalarWordObservation { e[0].Position += 4; return e },
		func(e []scalarWordObservation) []scalarWordObservation { e[0].Event.Effect.Src.Index++; return e },
	} {
		changed := mutate(slices.Clone(events))
		_, status, ok := auditScalarWordTrace(code, 0, len(code), changed, scalarWordAuditLimits{})
		if ok || status.Verdict != regalloccheck.Rejected {
			t.Fatal("bad event coverage admitted", status)
		}
	}
	for _, word := range []uint32{0x00013423, 0x00010113} { // raw header store / SP write
		mutated := append(slices.Clone(code), 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(mutated[len(code):], word)
		_, status, ok := auditScalarWordTrace(mutated, 0, len(mutated), events, scalarWordAuditLimits{})
		if ok || status.Verdict != regalloccheck.Rejected {
			t.Fatal("unobserved instruction accepted", status)
		}
	}
	_, status, ok := auditScalarWordTrace(code, 0, len(code)-4, events, scalarWordAuditLimits{})
	if ok || status.Verdict != regalloccheck.Rejected {
		t.Fatal("truncated byte coverage accepted", status)
	}
}

func TestScalarMachineWordUnsupportedAndBudgets(t *testing.T) {
	if _, ok := decodeScalarMachineWord(0x02029293); !ok {
		t.Fatal("valid RV64 shift count32 rejected")
	}
	for _, word := range []uint32{
		0, 1, 0xffffffff, // illegal/compressed
		0x04029293,         // reserved SLLI high bits (bit25 is a legal RV64 count bit; bit26 is not)
		0x0202929b,         // SLLIW count32 is reserved
		0x00211023,         // unsupported SH
		0x00011283,         // unsupported LH
		0x00000073,         // ECALL
		0x0000000f,         // FENCE outside this scalar recipe subset
		0x100132af,         // LR.D outside this scalar recipe subset
		0x0200103b,         // reserved MULHW-style word operation
		0x40007033,         // reserved OP funct7/funct3
		0x00002063,         // reserved branch condition
		0x000000e7 | 1<<12, // reserved JALR funct3
	} {
		if _, ok := decodeScalarMachineWord(word); ok {
			t.Fatalf("unknown word treated as admitted: %#x", word)
		}
		code := make([]byte, 4)
		binary.LittleEndian.PutUint32(code, word)
		_, status, ok := auditScalarWordTrace(code, 0, 4, nil, scalarWordAuditLimits{})
		if ok || status.Verdict != regalloccheck.Inconclusive || status.Reason != regalloccheck.UnsupportedOperation {
			t.Fatal("unknown word did not stop trace audit", word, status)
		}
	}
	code, events := scalarWordCapture(t, 0, func(a *enc.Asm) { a.ZeroExtend32(enc.X5, enc.X6) })
	for _, limits := range []scalarWordAuditLimits{{Words: 1}, {Events: 1}, {Work: 1}, {Work: 7}} {
		_, status, ok := auditScalarWordTrace(code, 0, len(code), events, limits)
		if ok || status.Reason != regalloccheck.ResourceLimit {
			t.Fatal("audit cap bypassed", limits, status)
		}
	}
	_, status, ok := auditScalarWordTrace(code, 0, len(code), events, scalarWordAuditLimits{Words: -1})
	if ok || status.Reason != regalloccheck.InvalidGraph {
		t.Fatal(status)
	}
	for _, span := range [][2]int{{-4, 4}, {1, 4}, {0, 5}, {4, 0}, {0, 12}} {
		_, status, ok := auditScalarWordTrace(code, span[0], span[1], events, scalarWordAuditLimits{})
		if ok || status.Reason != regalloccheck.UnsupportedOperation {
			t.Fatal(span, status)
		}
	}
	if summary, _, ok := auditScalarWordTrace(nil, 0, 0, nil, scalarWordAuditLimits{}); !ok || summary != (scalarWordAuditSummary{}) {
		t.Fatal("empty scoped trace", summary, ok)
	}
}

func TestScalarMachineWordTraceDoesNotAuthenticateRecipesOrFreezeBytes(t *testing.T) {
	code, events := scalarWordCapture(t, 0, func(a *enc.Asm) {
		a.Sd(enc.X30, enc.X31, 0) // no SP-relative event
		a.JumpLong(false)
	})
	scalarWordAuditOK(t, code, 0, events)
	// Dynamic-store source and branch-immediate mutations can preserve an
	// encoder trace. Future recipe/branch/immutable-receipt checks MUST reject
	// bad semantics; this prerequisite must not claim to supply those proofs.
	binary.LittleEndian.PutUint32(code, binary.LittleEndian.Uint32(code)^1<<20)
	binary.LittleEndian.PutUint32(code[4:], binary.LittleEndian.Uint32(code[4:])^1<<12)
	summary := scalarWordAuditOK(t, code, 0, events)
	if summary.Stores != 1 || summary.Controls != 1 {
		t.Fatal(summary)
	}
	d, _ := decodeScalarMachineWord(binary.LittleEndian.Uint32(code))
	if d.Register != 31 {
		t.Fatal("actual mutated store operand hidden", d)
	}
	// A literal no-op is fully scanned despite producing no observation.
	code = append(code, 0x13, 0, 0, 0)
	if got := scalarWordAuditOK(t, code, 0, events); got.Words != 4 {
		t.Fatal(got)
	}
}

func TestScalarMachineWordAuditNoMutationOrBackingAllocation(t *testing.T) {
	code, events := scalarWordCapture(t, 0, func(a *enc.Asm) {
		a.Constant64(enc.X31, 65536)
		a.Add(enc.X31, enc.SP, enc.X31)
		a.Lwu(enc.X5, enc.X31, 0)
		a.ZeroExtend32(enc.X6, enc.X5)
	})
	originalCode, originalEvents := slices.Clone(code), slices.Clone(events)
	if allocations := testing.AllocsPerRun(20, func() {
		if _, _, ok := auditScalarWordTrace(code, 0, len(code), events, scalarWordAuditLimits{}); !ok {
			panic("admitted trace failed")
		}
	}); allocations != 0 {
		t.Fatal("audit backing allocations", allocations)
	}
	if !slices.Equal(code, originalCode) || !slices.Equal(events, originalEvents) {
		t.Fatal("audit mutated its input")
	}
}
