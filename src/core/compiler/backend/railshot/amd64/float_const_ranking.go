//go:build amd64

package amd64

import (
	"os"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var floatConstRankingEnabled = os.Getenv("WAGO_AMD64_FLOAT_CONST_RANKING") == "1"

const floatConstRankingMaxBytes = 4096

// Port ARM64's conservative frequency choice without changing cache ownership:
// both registers are initialized at entry and remain reserved. Larger bodies
// keep the first-two scan. Thirty-two distinct candidates fit fixed scratch;
// overflow restores first-seen choices rather than ranking an incomplete set.
func (f *fn) rankFloatConsts(code []byte) (choice [2]storage, count int, changed bool) {
	var candidates [32]struct {
		bits int64
		n    uint16
		typ  machineType
	}
	n := 0
	first := func() {
		count = min(n, len(choice))
		for i := 0; i < count; i++ {
			choice[i] = storage{kind: stConst, typ: candidates[i].typ, cval: candidates[i].bits}
		}
	}
	r := wasm.NewReader(code)
	var imm wasm.InstructionImmediate
	for r.HasNext() {
		op, err := r.Byte()
		if err != nil {
			return
		}
		var typ machineType
		var bits int64
		switch op {
		case 0x43:
			value, err := r.LEU32()
			if err != nil {
				return choice, 0, false
			}
			typ, bits = mtF32, int64(value)
		case 0x44:
			value, err := r.LEU64()
			if err != nil {
				return choice, 0, false
			}
			typ, bits = mtF64, int64(value)
		default:
			if err := f.classifier.ClassifyInto(r, op, &imm); err != nil {
				return choice, 0, false
			}
			continue
		}
		at := 0
		for at < n && (candidates[at].typ != typ || candidates[at].bits != bits) {
			at++
		}
		if at == n {
			if n == len(candidates) {
				first()
				return
			}
			candidates[at].typ, candidates[at].bits = typ, bits
			n++
		}
		candidates[at].n++
	}
	first()
	if n <= 2 {
		return
	}
	best := [2]int{-1, -1}
	for i := 0; i < n; i++ {
		if best[0] < 0 || candidates[i].n > candidates[best[0]].n {
			best[1], best[0] = best[0], i
		} else if best[1] < 0 || candidates[i].n > candidates[best[1]].n {
			best[1] = i
		}
	}
	// Static occurrence count is only a proxy for loop heat. Preserve the old
	// order unless the new pair has at least twice the combined occurrences.
	if int(candidates[best[0]].n)+int(candidates[best[1]].n) < 2*(int(candidates[0].n)+int(candidates[1].n)) {
		return
	}
	for i, at := range best {
		choice[i] = storage{kind: stConst, typ: candidates[at].typ, cval: candidates[at].bits}
	}
	changed = best != [2]int{0, 1}
	return
}
