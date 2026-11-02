package main

import (
	"encoding/binary"
	"fmt"

	a64 "github.com/wago-org/wago/src/core/encoder/arm64"
)

const imm19Mask uint32 = 0x7ffff << 5

func word(a *a64.Asm, at int) uint32 { return binary.LittleEndian.Uint32(a.B[at:]) }

func clearImmediate(a *a64.Asm, at int) uint32 {
	before := word(a, at)
	a.PatchU32(at, before &^ imm19Mask)
	return before
}

func displacement(w uint32) int32 { return int32(w<<8) >> 13 }

func require(ok bool, format string, args ...any) {
	if !ok {
		panic(fmt.Sprintf(format, args...))
	}
}

func main() {
	var cb a64.Asm
	at := cb.Cbnz64(a64.X17)
	require(cb.PatchBranch19(at, at+4), "initial +4 patch failed")
	fallback := word(&cb, at)
	require(fallback == 0xb5000031, "initial word %#08x", fallback)

	clearImmediate(&cb, at)
	require(cb.PatchBranch19(at, at+0x440), "positive retarget failed")
	positive := word(&cb, at)
	require(positive == 0xb5002211, "positive word %#08x", positive)
	require(displacement(positive) == 0x110, "positive imm %d", displacement(positive))
	require(positive&^imm19Mask == fallback&^imm19Mask, "CBNZ opcode/register changed")

	clearImmediate(&cb, at)
	require(cb.PatchBranch19(at, at-0x440), "negative retarget failed")
	negative := word(&cb, at)
	require(displacement(negative) == -0x110, "negative imm %d", displacement(negative))
	require(negative&^imm19Mask == fallback&^imm19Mask, "negative CBNZ opcode/register changed")

	beforeRange := append([]byte(nil), cb.B...)
	require(!cb.PatchBranch19(at, at+(1<<20)), "out-of-range patch unexpectedly succeeded")
	require(string(beforeRange) == string(cb.B), "range failure modified bytes")

	var cond a64.Asm
	condAt := cond.Bcond(a64.CondGE)
	condBase := word(&cond, condAt)
	require(cond.PatchBranch19(condAt, condAt+8), "condition patch failed")
	condWord := word(&cond, condAt)
	require(condWord&^imm19Mask == condBase, "condition/opcode changed: %#08x -> %#08x", condBase, condWord)
	require(displacement(condWord) == 2, "condition displacement %d", displacement(condWord))

	for _, w := range []uint32{positive, negative, condWord} {
		require(displacement(w)*4%4 == 0, "decoded target not instruction aligned")
	}

	fmt.Printf("PASS fallback=%#08x positive=%#08x negative=%#08x condition=%#08x range_failure_unchanged=true\n", fallback, positive, negative, condWord)
}
