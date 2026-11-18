// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build arm64

package arm64

import (
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"os"
)

var byteStorePairEnabled = os.Getenv("WAGO_ARM64_NO_BYTE_STORE_PAIR") != "1"

// Match two scalar bytes of the same local at consecutive wrapping i32
// addresses. The complete two-byte access must be statically valid: otherwise
// the first byte must remain visible if the second store traps. Shared memory
// and all intervening effects decline. The original first address still runs.
func (f *fn) inspectByteStorePair(r *wasm.Reader, offset uint64) (int, bool) {
	if !f.opt(optByteStorePair) || !f.opt(optMemoryMinimumBounds) || f.moduleEH || len(f.customInstructions) != 0 {
		return 0, false
	}
	mt, ok := f.memoryType(0)
	if !ok || mt.Shared || mt.Limits.Addr64 {
		return 0, false
	}
	value := f.s.back()
	if value == nil || value.elemKind() != ekValue || (value.st.typ != mtI32 && value.st.typ != mtI64) {
		return 0, false
	}
	address := f.s.prev(f.s.baseOfValentBlock(value))
	if address == nil || !address.isDeferred() || address.st.typ != mtI32 || address.deferredOp() != opAdd {
		return 0, false
	}
	base, bias := f.s.arg0(address), f.s.arg1(address)
	if base == nil || base.elemKind() != ekValue || base.st.typ != mtI32 || (base.st.kind != stLocalRef && base.st.kind != stLocalReg) || bias == nil || bias.elemKind() != ekValue || bias.st.kind != stConst || bias.st.typ != mtI32 {
		return 0, false
	}
	scan := *r
	next := func(want byte) bool { op, e := scan.Byte(); return e == nil && op == want }
	if !next(0x20) {
		return 0, false
	}
	index, e := scan.U32()
	if e != nil || int(index)+f.localBase != int(base.st.idx) || !next(0x41) {
		return 0, false
	}
	second, e := scan.I32()
	if e != nil || uint32(second) != uint32(bias.st.cval)+1 || !next(0x6a) || !next(0x20) {
		return 0, false
	}
	local, e := scan.U32()
	x := int(local) + f.localBase
	if e != nil || x < 0 || x >= f.nLocals || f.localType[x] != value.st.typ {
		return 0, false
	}
	same := false
	switch value.st.kind {
	case stLocalRef, stLocalReg:
		same = int(value.st.idx) == x
	case stReg:
		// Only an unpinned local with a current canonical home can be
		// identified through its recent frame-store fact.
		if f.locals[x].reg != regNone || f.locals[x].state != lsMem {
			return 0, false
		}
		reg, valid := f.frameForwardSource(f.localOff(x))
		same = valid && reg == value.st.reg
	}
	if !same {
		return 0, false
	}
	constant, shift, store := byte(0x41), byte(0x76), byte(0x3a)
	if value.st.typ == mtI64 {
		constant, shift, store = 0x42, 0x88, 0x3c
	}
	if !next(constant) {
		return 0, false
	}
	if constant == 0x41 {
		n, e := scan.I32()
		if e != nil || n != 8 {
			return 0, false
		}
	} else {
		n, e := scan.I64()
		if e != nil || n != 8 {
			return 0, false
		}
	}
	if !next(shift) || !next(store) {
		return 0, false
	}
	memory, off, e := f.readMemArg(&scan)
	if e != nil || memory != 0 || off != offset {
		return 0, false
	}
	budget := 32
	maximum := f.unsignedAddressMaximum(address, false, &budget)
	if !shared.ProvenMinimumAccess(mt, maximum, offset, 2) {
		return 0, false
	}
	return scan.Offset(), true
}
