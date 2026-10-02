package amd64

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestDenseRotateBody(t *testing.T) {
	classifier := wasm.NewModuleInstructionClassifier(&wasm.Module{}, true)

	t.Run("below crossover", func(t *testing.T) {
		body := make([]byte, denseRorxOpCrossover-1)
		for i := range body {
			body[i] = 0x78 // i32.rotr
		}
		if denseRotateBody(body, classifier) {
			t.Fatal("classified below-crossover body as dense")
		}
	})

	t.Run("at crossover", func(t *testing.T) {
		body := make([]byte, denseRorxOpCrossover)
		for i := range body {
			body[i] = 0x89 // i64.rotl
		}
		if !denseRotateBody(body, classifier) {
			t.Fatal("did not classify crossover body as dense")
		}
	})

	t.Run("immediate bytes are skipped", func(t *testing.T) {
		body := make([]byte, 0, 2*(denseRorxOpCrossover+1))
		for range denseRorxOpCrossover + 1 {
			body = append(body, 0x41, 0x77) // i32.const -9; 0x77 is i32.rotl as an opcode.
		}
		if denseRotateBody(body, classifier) {
			t.Fatal("counted rotate opcode values inside immediates")
		}
	})

	t.Run("malformed fails closed", func(t *testing.T) {
		body := append(make([]byte, denseRorxOpCrossover-1), 0x20) // truncated local.get
		if denseRotateBody(body, classifier) {
			t.Fatal("classified malformed body as dense")
		}
	})
}

func TestDenseRotateRegionalBody(t *testing.T) {
	classifier := wasm.NewModuleInstructionClassifier(&wasm.Module{}, true)
	body := make([]byte, denseRorxBodyCrossover)
	for i := range denseRorxOpCrossover {
		body[i] = 0x78 // i32.rotr
	}
	scores := make([]uint32, minIntervalRegionLocals)
	lastGets := make([]uint32, len(scores))
	types := make([]machineType, len(scores))
	for i := range scores {
		scores[i], lastGets[i], types[i] = 2, 1, mtI32
	}
	hints := funcHintView{
		funcHints:    funcHints{flags: hintIntervalRegionStorage, localCount: uint16(len(scores))},
		nLocals:      len(scores),
		localScore:   scores,
		localLastGet: lastGets,
	}
	if !denseRotateRegionalBody(body, &hints, types, classifier) {
		t.Fatal("eligible high-pressure rotate kernel was rejected")
	}

	scores[0] = 1
	if denseRotateRegionalBody(body, &hints, types, classifier) {
		t.Fatal("kernel below the regional pressure threshold was admitted")
	}
}
