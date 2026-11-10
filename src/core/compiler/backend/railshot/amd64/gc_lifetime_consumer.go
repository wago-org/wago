// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build amd64

package amd64

import (
	"errors"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

var gcLifetimeEmissionTestHook func(*fn, int) error

func (f *fn) emitGCLifetime(p *gcLifetimePlan) error {
	st := p.context.fact.typ
	var emitErr error
	emittedEnd := false
	emitted := 0
	ok := p.walk(func(rec gcLifetimeEmissionRecord) bool {
		f.wasmPC = f.tracePCBase + uint32(rec.pc)
		switch rec.kind {
		case 1:
			if rec.op == 0x0b {
				r := wasm.ReaderFrom(p.body)
				_ = r.JumpTo(rec.pc)
				emitErr = f.bodyLoop(&r, 0)
				emittedEnd = true
				return false
			}
			r := wasm.ReaderFrom(p.body[rec.pc+1 : rec.end])
			if err := f.emitPlain(&r, rec.op); err != nil {
				emitErr = err
				return false
			}
		case 2:
			f.pushGCForwardedValue(rec.value)
		case 3:
			for _, field := range st.Comp.Fields {
				t := field.Storage().Val()
				if rec.defaultNew {
					f.pushValue(zeroStorage(lifetimeMachineType(t)))
				}
			}
			// The compact replay has proved that the identity and payload never
			// escape. Keep the initializer values virtual, but preserve Wago's
			// observable allocation effects through the dedicated dead charge.
			f.pushValue(storage{kind: stConst, typ: mtI32, cval: int64(p.typ)})
			f.nativeStructAllocType = p.typ + 1
			f.nativeStructAllocDead = true
			result := wasm.RefVal(wasm.Ref(false, wasm.IndexedHeap(wasm.TypeIdx{Index: p.typ}), false))
			if err := f.callGCStructHelper(gcStructReserveDeadNative, []wasm.ValType{wasm.I32}, []wasm.ValType{result}); err != nil {
				emitErr = err
				return false
			}
			f.dropValue()
			f.discardGCConstructorOperands(len(st.Comp.Fields))
			f.stats.peep("gc-lifetime-checked-scalar-replace")
		}
		emitted++
		if gcLifetimeEmissionTestHook != nil {
			if err := gcLifetimeEmissionTestHook(f, emitted); err != nil {
				emitErr = err
				return false
			}
		}
		return true
	})
	if emitErr != nil {
		return emitErr
	}
	if emittedEnd {
		f.gcLifetimeSelected = true
		if f.opt(optPreparedBoundedEntry) && len(f.localType) <= 128 {
			// The all-ones marker identifies the checked scalar lifetime wrapper
			// without growing Compiled or Instance.
			f.gcDeadBatchLayout = ^uint64(0)
		}
		return nil
	}
	if !ok {
		return errors.New("gc lifetime emission replay mismatch")
	}
	f.gcLifetimeSelected = true
	if f.opt(optPreparedBoundedEntry) && len(f.localType) <= 128 {
		f.gcDeadBatchLayout = ^uint64(0)
	}
	return nil
}
