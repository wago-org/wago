//go:build wago_regalloccheck

package regalloccheck

import (
	"fmt"
	"strings"
	"testing"
)

func rejects(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if err := recover(); err == nil || !strings.Contains(fmt.Sprint(err), "regalloccheck:") {
			t.Fatalf("expected checker failure, got %v", err)
		}
	}()
	fn()
}

func TestSpillReloadCallAndRegisterBanks(t *testing.T) {
	var s State
	gp, fp := Register(GP, 4), Register(FP, 4)
	a, b := s.Seed(gp, 8), s.Seed(fp, 16)
	s.Apply(Effect{Kind: Copy, Dst: Slot(40), Src: gp, Size: 8})
	s.Apply(Effect{Kind: Copy, Dst: Slot(48), Src: fp, Size: 16})
	s.Apply(Effect{Kind: Call})
	rejects(t, func() { s.Expect("clobbered GP", gp, a) })
	rejects(t, func() { s.Expect("clobbered FP", fp, b) })
	s.Apply(Effect{Kind: Copy, Dst: gp, Src: Slot(40), Size: 8})
	s.Apply(Effect{Kind: Copy, Dst: fp, Src: Slot(48), Size: 16})
	s.Expect("restored GP", gp, a)
	s.Expect("restored FP", fp, b)
}

func TestWrongSlotAndPartialVectorOverwrite(t *testing.T) {
	var s State
	a := s.Seed(Slot(0), 16)
	s.Seed(Slot(32), 16)
	s.Apply(Effect{Kind: Copy, Dst: Register(FP, 1), Src: Slot(32), Size: 16})
	rejects(t, func() { s.Expect("wrong reload", Register(FP, 1), a) })
	s.Apply(Effect{Kind: Copy, Dst: Slot(8), Src: Slot(32), Size: 8})
	rejects(t, func() { s.Expect("overwritten upper vector half", Slot(0), a) })
}

func TestJoinsIntersectFactsAndKeepDistinctDefinitions(t *testing.T) {
	var entry State
	loc := Register(GP, 1)
	before := entry.Seed(loc, 8)
	left, right := entry.Clone(), entry.Clone()
	left.Put(Slot(8), left.Fresh(8))
	right.Put(Slot(8), right.Fresh(8))
	left.Meet(right)
	left.Expect("unchanged on both edges", loc, before)
	rejects(t, func() { left.ExpectKnown("different edge definitions", Slot(8), right.Read(Slot(8), 8)) })
	right.Apply(Effect{Kind: Call})
	left.Meet(right)
	rejects(t, func() { left.Expect("one edge clobbered", loc, before) })
}

func TestUnknownNeverProvesOwnership(t *testing.T) {
	var s State
	rejects(t, func() { s.Expect("unknown", Slot(0), s.Read(Slot(0), 8)) })
	rejects(t, func() { s.ExpectKnown("unknown", Slot(0), s.Read(Slot(0), 8)) })
}

func TestGP32WriteInvalidatesOldWideIdentity(t *testing.T) {
	var s State
	reg := Register(GP, 1)
	wide := s.Seed(reg, 8)
	s.Apply(Effect{Kind: Copy, Dst: reg, Src: reg, Size: 4})
	s.Expect("low bits", reg, wide[:4])
	rejects(t, func() { s.Expect("zero-extended carrier", reg, wide) })
}
