//go:build amd64

package amd64

import (
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func TestImmutableIntegerCacheExcludesFixedBulkScratch(t *testing.T) {
	before := wideLoopIntConstEnabled
	wideLoopIntConstEnabled = true
	defer func() { wideLoopIntConstEnabled = before }()
	all := maskOf(R12, R13, R14, R15, R9, R10, R11, RDI, RSI)
	for _, operation := range []struct {
		name  string
		flags funcHintFlags
	}{
		{"ordinary", 0}, {"memory", hintUsesBulkMem}, {"table", hintMutatesTable}, {"inlined-call", hintHasCall},
	} {
		for _, candidate := range []Reg{RDI, RSI, R9, R10} {
			t.Run(fmt.Sprintf("%s/r%d", operation.name, candidate), func(t *testing.T) {
				h := funcHintView{loopIntConsts: &loopIntConstHintEntry{bits: [2]int64{0x123456789abcdef}, count: 1}}
				h.flags = operation.flags
				f := fn{a: &enc.Asm{}, reserved: all.remove(candidate), policy: currentCodegenPolicy()}
				f.preloadLoopIntConsts(&h)
				fixed := (candidate == RDI || candidate == RSI) && operation.flags.has(hintUsesBulkMem|hintMutatesTable|hintHasCall) || candidate == R9 && operation.flags.has(hintMutatesTable|hintHasCall)
				if fixed && f.iconstN != 0 {
					t.Fatalf("fixed scratch %v received immutable cache", candidate)
				}
				if !fixed && (f.iconstN != 1 || f.iconsts[0].reg != candidate) {
					t.Fatalf("safe candidate %v unexpectedly rejected", candidate)
				}
			})
		}
	}
}

func TestMemoryInitMarksFixedBulkScratch(t *testing.T) {
	byteHints, err := scanBodyBytes([]byte{0xfc, 0x08, 0x00, 0x00, 0x0b}, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	astHints := scanBody(wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrMemoryInit}}}, 0, 0, 0)
	for name, h := range map[string]funcHintView{"bytes": byteHints, "AST": astHints} {
		if !h.flags.has(hintUsesBulkMem) || !h.flags.has(hintTouchesMemory) {
			t.Errorf("%s memory.init lost bulk scratch hint", name)
		}
	}
}
