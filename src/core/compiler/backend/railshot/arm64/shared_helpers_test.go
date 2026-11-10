//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"testing"
)

func TestSharedIntegerSemanticCodes(t *testing.T) {
	if uint8(opNone) != uint8(shared.IntNone) {
		t.Fatal("None semantic code mismatch")
	}
	if uint8(opAdd) != uint8(shared.IntAdd) {
		t.Fatal("Add semantic code mismatch")
	}
	if uint8(opSub) != uint8(shared.IntSub) {
		t.Fatal("Sub semantic code mismatch")
	}
	if uint8(opAnd) != uint8(shared.IntAnd) {
		t.Fatal("And semantic code mismatch")
	}
	if uint8(opOr) != uint8(shared.IntOr) {
		t.Fatal("Or semantic code mismatch")
	}
	if uint8(opXor) != uint8(shared.IntXor) {
		t.Fatal("Xor semantic code mismatch")
	}
	if uint8(opShl) != uint8(shared.IntShl) {
		t.Fatal("Shl semantic code mismatch")
	}
	if uint8(opShrU) != uint8(shared.IntShrU) {
		t.Fatal("ShrU semantic code mismatch")
	}
	if uint8(opShrS) != uint8(shared.IntShrS) {
		t.Fatal("ShrS semantic code mismatch")
	}
	if uint8(opRotl) != uint8(shared.IntRotl) {
		t.Fatal("Rotl semantic code mismatch")
	}
	if uint8(opRotr) != uint8(shared.IntRotr) {
		t.Fatal("Rotr semantic code mismatch")
	}
	if uint8(opMul) != uint8(shared.IntMul) {
		t.Fatal("Mul semantic code mismatch")
	}
	if uint8(opDivU) != uint8(shared.IntDivU) {
		t.Fatal("DivU semantic code mismatch")
	}
	if uint8(opDivS) != uint8(shared.IntDivS) {
		t.Fatal("DivS semantic code mismatch")
	}
	if uint8(opRemU) != uint8(shared.IntRemU) {
		t.Fatal("RemU semantic code mismatch")
	}
	if uint8(opRemS) != uint8(shared.IntRemS) {
		t.Fatal("RemS semantic code mismatch")
	}
	if uint8(opClz) != uint8(shared.IntClz) {
		t.Fatal("Clz semantic code mismatch")
	}
	if uint8(opCtz) != uint8(shared.IntCtz) {
		t.Fatal("Ctz semantic code mismatch")
	}
	if uint8(opPopcnt) != uint8(shared.IntPopcnt) {
		t.Fatal("Popcnt semantic code mismatch")
	}
	if uint8(opWrap) != uint8(shared.IntWrap) {
		t.Fatal("Wrap semantic code mismatch")
	}
	if uint8(opSExt32) != uint8(shared.IntSExt32) {
		t.Fatal("SExt32 semantic code mismatch")
	}
	if uint8(opZExt32) != uint8(shared.IntZExt32) {
		t.Fatal("ZExt32 semantic code mismatch")
	}
	if uint8(opSExt8) != uint8(shared.IntSExt8) {
		t.Fatal("SExt8 semantic code mismatch")
	}
	if uint8(opSExt16) != uint8(shared.IntSExt16) {
		t.Fatal("SExt16 semantic code mismatch")
	}
	if uint8(opEq) != uint8(shared.IntEq) {
		t.Fatal("Eq semantic code mismatch")
	}
	if uint8(opNe) != uint8(shared.IntNe) {
		t.Fatal("Ne semantic code mismatch")
	}
	if uint8(opLtS) != uint8(shared.IntLtS) {
		t.Fatal("LtS semantic code mismatch")
	}
	if uint8(opLtU) != uint8(shared.IntLtU) {
		t.Fatal("LtU semantic code mismatch")
	}
	if uint8(opGtS) != uint8(shared.IntGtS) {
		t.Fatal("GtS semantic code mismatch")
	}
	if uint8(opGtU) != uint8(shared.IntGtU) {
		t.Fatal("GtU semantic code mismatch")
	}
	if uint8(opLeS) != uint8(shared.IntLeS) {
		t.Fatal("LeS semantic code mismatch")
	}
	if uint8(opLeU) != uint8(shared.IntLeU) {
		t.Fatal("LeU semantic code mismatch")
	}
	if uint8(opGeS) != uint8(shared.IntGeS) {
		t.Fatal("GeS semantic code mismatch")
	}
	if uint8(opGeU) != uint8(shared.IntGeU) {
		t.Fatal("GeU semantic code mismatch")
	}
	if uint8(opEqz) != uint8(shared.IntEqz) {
		t.Fatal("Eqz semantic code mismatch")
	}
}
