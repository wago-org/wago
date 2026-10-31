//go:build amd64

package amd64

import (
	"math/rand"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func checkLoopScanDecode49(t *testing.T, body []byte) {
	t.Helper()
	classifiers := []wasm.ModuleInstructionClassifier{
		wasm.NewModuleInstructionClassifier(nil, false),
		wasm.NewModuleInstructionClassifier(nil, true),
		wasm.NewModuleInstructionClassifier(&wasm.Module{Memories: []wasm.MemType{{Limits: wasm.Limits{Addr64: true}}}}, true),
		wasm.NewModuleInstructionClassifier(&wasm.Module{Memories: []wasm.MemType{{}, {Limits: wasm.Limits{Addr64: true}}}}, true),
	}
	for _, classifier := range classifiers {
		for _, gc := range []bool{false, true} {
			for _, wantFloat := range []bool{false, true} {
				for _, initial := range []int{0, maxCallFreeLoopLookaheadBytes - len(body), maxCallFreeLoopLookaheadBytes - 1, maxCallFreeLoopLookaheadBytes} {
					encoded := append([]byte{0x01}, body...)
					a, b := wasm.ReaderFrom(encoded), wasm.ReaderFrom(encoded)
					_ = a.Step(1)
					_ = b.Step(1)
					budgetA, budgetB := initial, initial
					pins := []int{0, 2, 5, 32768, -1}
					okA, writesA, stA, floatA := referenceLoopCallFreeDetails49(&a, classifier, gc, pins, wantFloat, &budgetA)
					okB, writesB, stB, floatB := scanLoopCallFreeDetails(&b, classifier, gc, pins, wantFloat, &budgetB)
					if okA != okB || writesA != writesB || stA != stB || floatA != floatB || budgetA != budgetB || a.Offset() != 1 || b.Offset() != 1 {
						t.Fatalf("body=%x gc=%t wantFloat=%t initial=%d: reference=%t/%x/%+v/%t/%d new=%t/%x/%+v/%t/%d", body, gc, wantFloat, initial, okA, writesA, stA, floatA, budgetA, okB, writesB, stB, floatB, budgetB)
					}
				}
			}
		}
	}
}

func loopScanDecodeSeeds49() [][]byte {
	seeds := [][]byte{
		{}, {0x0b}, {0x21, 2, 0x22, 5, 0x0b},
		{0x20, 0, 0x23, 0, 0x24, 0, 0x0c, 0, 0x0d, 0, 0xd2, 0, 0xd5, 0, 0xd6, 0, 0x0b},
		{0x41, 0xff, 0x7f, 0x42, 0x80, 0x01, 0x2b, 0x40, 1, 0xff, 0xff, 0xff, 0xff, 0x10, 0x0b},
		{0x43, 0, 0, 0x80, 0x3f, 0x44, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x0b},
		{0x02, 0x40, 0x21, 2, 0x43, 0, 0, 0, 0x80, 0x0b, 0x0b},
		{0x03, 0x40, 0x21, 2, 0x0b, 0x0b},
		{0x04, 0x40, 0x21, 2, 0x05, 0x22, 5, 0x0b, 0x0b},
		{0x21, 2, 0x0e, 1, 0, 0, 0x0b},
		{0x10, 0, 0x0b}, {0x12, 0, 0x0b}, {0x14, 0, 0x0b}, {0x15, 0, 0x0b},
		{0x11, 0, 0, 0x0b}, {0x13, 0, 0, 0x0b}, {0x40, 0, 0x0b},
		{0x26, 0, 0x43, 0, 0, 0x80, 0x3f, 0x0b},
		{0xfc, 10, 0, 0, 0x43, 0, 0, 0x80, 0x3f, 0x0b},
		{0xfb, 0x14, 0x6e, 0x0b}, {0xfe, 1, 2, 0, 0x0b},
		{0xfd, 0x0c, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x0b},
		{0x1c, 1, 0x7b, 0x0b},
	}
	for _, count := range []int{255, 256, 257} {
		body := make([]byte, count)
		for i := range body {
			body[i] = 0x6a
		}
		seeds = append(seeds, append(body, 0x43, 0, 0, 0x80, 0x3f, 0x0b))
	}
	var floats []byte
	for i := 0; i < 20; i++ {
		floats = append(floats, 0x43, byte(i), 0, 0x80, 0x3f)
	}
	return append(seeds, append(floats, 0x0b))
}

func TestLoopScanDecodeMatchesReference49(t *testing.T) {
	for _, body := range loopScanDecodeSeeds49() {
		checkLoopScanDecode49(t, body)
		for end := 0; end < len(body); end++ {
			checkLoopScanDecode49(t, body[:end])
		}
	}
	rng := rand.New(rand.NewSource(49))
	for opcode := 0; opcode < 256; opcode++ {
		for size := 0; size < 12; size++ {
			body := make([]byte, size+1)
			body[0] = byte(opcode)
			_, _ = rng.Read(body[1:])
			checkLoopScanDecode49(t, body)
		}
	}
}

func FuzzLoopScanDecodeMatchesReference49(f *testing.F) {
	for _, body := range loopScanDecodeSeeds49() {
		f.Add(body)
	}
	f.Fuzz(func(t *testing.T, body []byte) { checkLoopScanDecode49(t, body) })
}

// Reference retained from candidate48 before specializing scalar decoding.
func referenceLoopCallFreeDetails49(r *wasm.Reader, classifier wasm.ModuleInstructionClassifier, gcStructHelpers bool, pinnedLocals []int, wantFloat bool, budget *int) (bool, uint64, storage, bool) {
	if budget == nil || *budget >= maxCallFreeLoopLookaheadBytes {
		return false, 0, storage{}, false
	}
	r2 := *r
	depth := 0
	var writes uint64
	brTable := false
	nestedLoop := false
	hasInnerControl := false
	type candidate struct {
		st    storage
		score uint32
	}
	var candidates [16]candidate
	n, steps := 0, 0
	floatSafe := true
	finish := func(writes uint64) (bool, uint64, storage, bool) {
		if !wantFloat || !floatSafe || n == 0 {
			return true, writes, storage{}, false
		}
		best := 0
		for i := 1; i < n; i++ {
			if candidates[i].score > candidates[best].score {
				best = i
			}
		}
		return true, writes, candidates[best].st, true
	}
	var imm wasm.InstructionImmediate
	for {
		before := r2.Offset()
		op, err := r2.Byte()
		constReader := r2
		if err != nil {
			return false, 0, storage{}, false
		}
		// Immediate-free instructions cannot write a local, introduce control,
		// select a float literal, or call a GC/atomic helper. Keep their proof
		// step scalar instead of clearing and filling an unused immediate record.
		if _, simple := wasm.ImmediateFreeInstructionKind(op); simple {
			if *budget == maxCallFreeLoopLookaheadBytes {
				return false, 0, storage{}, false
			}
			*budget++
			steps++
			continue
		}
		if err = classifier.ClassifyInto(&r2, op, &imm); err != nil {
			used := min(maxCallFreeLoopLookaheadBytes-*budget, r2.Offset()-before)
			*budget += used
			return false, 0, storage{}, false
		}
		consumed := r2.Offset() - before
		remaining := maxCallFreeLoopLookaheadBytes - *budget
		if consumed > remaining {
			*budget = maxCallFreeLoopLookaheadBytes
			return false, 0, storage{}, false
		}
		*budget += consumed
		if wantFloat && steps < 256 && (op == 0x43 || op == 0x44) {
			var st storage
			if op == 0x43 {
				bits, err := constReader.LEU32()
				if err != nil {
					return false, 0, storage{}, false
				}
				st = storage{kind: stConst, typ: mtF32, cval: int64(bits)}
			} else {
				bits, err := constReader.LEU64()
				if err != nil {
					return false, 0, storage{}, false
				}
				st = storage{kind: stConst, typ: mtF64, cval: int64(bits)}
			}
			weight := uint32(1) << min(depth*2, 8)
			found := false
			for i := 0; i < n; i++ {
				if candidates[i].st.typ == st.typ && candidates[i].st.cval == st.cval {
					candidates[i].score += weight
					found = true
					break
				}
			}
			if !found && n < len(candidates) {
				candidates[n] = candidate{st: st, score: weight}
				n++
			}
		}
		steps++
		if op == 0x02 || op == 0x04 || op == 0x1f {
			hasInnerControl = true
		}
		switch imm.Kind {
		case wasm.InstrCall, wasm.InstrCallIndirect, wasm.InstrReturnCall,
			wasm.InstrReturnCallIndirect, wasm.InstrCallRef, wasm.InstrReturnCallRef,
			wasm.InstrMemoryGrow:
			return false, 0, storage{}, false
		case wasm.InstrTableSet, wasm.InstrTableInit, wasm.InstrTableCopy,
			wasm.InstrTableGrow, wasm.InstrTableFill:
			floatSafe = false // GC table helpers may call even in a numeric loop.
		case wasm.InstrLocalSet, wasm.InstrLocalTee:
			for i, x := range pinnedLocals {
				if uint32(x) == imm.Index {
					writes |= uint64(1) << i
					break
				}
			}
		case wasm.InstrBrTable:
			// br_table first reconciles every pin to its frame slot. Its
			// per-target stubs then undo register-only loop state, often on
			// the hot path. Keep the existing stack convergence there.
			brTable = true
		}
		if gcOrAtomicInstructionMayCall(imm.Kind, gcStructHelpers) {
			return false, 0, storage{}, false
		}
		if imm.UsesBulkMemory {
			floatSafe = false
		}
		switch op {
		case 0x02, 0x03, 0x04, 0x1f:
			if op == 0x03 {
				nestedLoop = true
			}
			depth++
		case 0x0b:
			if depth == 0 {
				// Nested loop entries already reconcile their own pin state. An
				// outer register-only header can add stores at that inner entry
				// without removing stores on its hot backedge. Inner blocks and
				// ifs likewise create merge edges where the weaker register-only
				// state adds reconciliation outside the loop latch.
				if brTable || nestedLoop || hasInnerControl {
					return finish(0)
				}
				return finish(writes)
			}
			depth--
		}
	}
}
