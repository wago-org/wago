//go:build (linux || darwin || windows) && amd64 && !tinygo && !wago_profile

package amd64

import (
	"bytes"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/amd64"
	"testing"
)

func TestStreamRorxEncoding(t *testing.T) {
	old, oldDense := streamingRorxEnabled, streamingPreserveDenseEnabled
	streamingRorxEnabled, streamingPreserveDenseEnabled = true, true
	t.Cleanup(func() { streamingRorxEnabled, streamingPreserveDenseEnabled = old, oldDense })
	for _, op := range []byte{0x77, 0x78, 0x89, 0x8a} {
		for _, refs := range []uint16{1, 2} {
			for _, count := range []int64{0, 1, 31, 32, 63, 64, -1, -2147483648} {
				sc := newScratch()
				f := fn{a: sc.asm, sc: sc, bmi2Rorx: true}
				s := streamState{target: &f, regs: []uint8{uint8(R8), uint8(R9), uint8(R10)}, reserved: uint64(1) << RCX}
				s.add(streamNode{})
				wide, _ := streamRotateShape(op)
				value := s.add(streamNode{kind: shared.ScalarRegister, reg: uint8(R8), refs: refs, wide: wide, constant: 1})
				n := s.add(streamNode{kind: shared.ScalarConstant, constant: count, refs: 1, wide: wide})
				s.owners[R8] = value
				s.stack = []streamID{value, n}
				if err := s.integerRotate(op); err != nil {
					t.Fatal(err)
				}
				dst := R8
				if refs == 2 {
					dst = R9
				}
				mask := uint64(31)
				if wide {
					mask = 63
				}
				amount := uint64(count) & mask
				if op == 0x77 || op == 0x89 {
					amount = (-amount) & mask
				}
				var want encoder.Asm
				if refs == 2 {
					want.Rorx(dst, R8, byte(amount), wide)
				} else {
					compactAmount := byte(uint64(count) & mask)
					if !wide && compactAmount == 0 {
						want.MovRegReg32(R8, R8)
					}
					digit := byte(0)
					if op == 0x78 || op == 0x8a {
						digit = 1
					}
					want.ShiftImm(digit, R8, compactAmount, wide)
				}
				if !bytes.Equal(f.a.B, want.B) || f.a.UsesBMI2 != (refs == 2) {
					t.Fatalf("op=%x count=%d refs=%d got=%x want=%x", op, count, refs, f.a.B, want.B)
				}
				if refs == 2 && (s.owners[R8] != value || s.node(value).refs != 1) {
					t.Fatal("shared source changed")
				}
			}
		}
	}
}

func TestStreamRorxPolicyAndCPU(t *testing.T) {
	streamMemoryTestEnable(t)
	old, oldDense := streamingRorxEnabled, streamingPreserveDenseEnabled
	streamingRorxEnabled, streamingPreserveDenseEnabled = true, true
	t.Cleanup(func() { streamingRorxEnabled, streamingPreserveDenseEnabled = old, oldDense })
	m := mod1(t, []wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}, []byte{0, 0x20, 0, 0x41, 7, 0x78, 0x20, 0, 0x73, 0x0b})
	for _, features := range []shared.AMD64Features{0, shared.AMD64BMI2} {
		for _, enabled := range []bool{false, true} {
			var stats ModuleStats
			opts := CompileOptions{DeferCodeMapping: true, AMD64FeaturesSet: true, AMD64Features: features, Optimizations: map[string]bool{"bmi2-rorx": enabled}}
			if diagnosticsEnabled {
				opts.Stats = &stats
			}
			cm, err := CompileModuleWith(m, opts)
			if err != nil {
				t.Fatal(err)
			}
			if cm.CodeImage != nil {
				defer cm.CodeImage.Close()
			}
			if diagnosticsEnabled && !regallocCheckEnabled {
				if len(stats.Funcs) != 1 || stats.Funcs[0].Peephole["streaming-function"] != 1 {
					t.Fatal("test fell back instead of selecting stream")
				}
				got := stats.Funcs[0].Peephole["streaming-rotate-rorx"]
				want := uint64(0)
				if enabled && features.Has(shared.AMD64BMI2) {
					want = 1
				}
				if uint64(got) != want {
					t.Fatalf("stream RORX selection got=%d want=%d", got, want)
				}
			}
			if cm.RequiresBMI2 != (enabled && features.Has(shared.AMD64BMI2)) {
				t.Fatal("BMI2 policy/requirement mismatch")
			}
		}
	}
}

func TestStreamRorxBackingAliasPressure(t *testing.T) {
	old, oldDense := streamingRorxEnabled, streamingPreserveDenseEnabled
	streamingRorxEnabled, streamingPreserveDenseEnabled = true, true
	t.Cleanup(func() { streamingRorxEnabled, streamingPreserveDenseEnabled = old, oldDense })
	for _, count := range []int64{0, 7, 32, -1} {
		s := streamResidencyUnitState()
		s.target.bmi2Rorx = true
		cell := s.residency.homes[0]
		alias := s.borrowBacking(cell, 0)
		snapshot := s.operand(alias)
		value := s.locals[0]
		s.retain(value)
		// Fill ordinary registers so producing the result must evict/spill a lease.
		for _, r := range []uint8{uint8(RAX), uint8(RCX)} {
			id := s.add(streamNode{kind: shared.ScalarRegister, reg: r, refs: 1, wide: true})
			s.owners[r] = id
			s.stack = append(s.stack, id)
		}
		countID := s.add(streamNode{kind: shared.ScalarConstant, constant: count, refs: 1})
		s.stack = append(s.stack, value, countID)
		if err := s.integerRotate(0x77); err != nil {
			t.Fatal(err)
		}
		if !s.target.a.UsesBMI2 {
			t.Fatal("RORX not emitted")
		}
		original, detached := s.operand(s.locals[0]), s.operand(alias)
		// Materialization gives the ordinary local node a separate register.
		// Physical-location equality is not the snapshot contract: verify the
		// unchanged backing, live copied owner, and actual non-destructive copy.
		if detached != snapshot || s.node(alias).kind != streamBacking || uint32(s.node(alias).slot) != cell {
			t.Fatal("detached source generation changed")
		}
		if original.Kind != shared.ScalarRegister || s.owners[original.Reg] != value || s.node(value).refs != 1 {
			t.Fatal("materialized source copy lost its local owner")
		}
		var copyCode encoder.Asm
		copyCode.MovRegReg32(Reg(original.Reg), Reg(snapshot.Reg))
		if snapshot.Kind != shared.ScalarRegister || !bytes.Contains(s.target.a.B, copyCode.B) {
			t.Fatal("source snapshot was not copied into its ordinary owner")
		}
		result := s.stack[len(s.stack)-1]
		resultOp := s.operand(result)
		if resultOp.Kind != shared.ScalarRegister || resultOp.Reg == original.Reg || resultOp.Reg == snapshot.Reg {
			t.Fatal("shared rotate overwrote a source owner")
		}
		var rotateCode encoder.Asm
		rotateCode.Rorx(Reg(resultOp.Reg), Reg(original.Reg), byte((-uint64(count))&31), false)
		if !bytes.HasSuffix(s.target.a.B, rotateCode.B) {
			t.Fatal("rotation did not consume the preserved copy")
		}
		if s.registerBacking(result) != 0 {
			t.Fatal("rotated result retained obsolete backing proof")
		}
		if s.Spills == 0 {
			t.Fatal("fixture did not exercise pressure")
		}
	}
}
