// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

package wago

import "fmt"

type gatedI32ToI32HostFunc struct {
	fn   i32ToI32HostFunc
	gate *pluginCallGate
}

type gatedNoArgsHostFunc struct {
	fn   noArgsHostFunc
	gate *pluginCallGate
}

type gatedI32HostFunc struct {
	fn   i32HostFunc
	gate *pluginCallGate
}

type gatedI32I32HostFunc struct {
	fn   i32I32HostFunc
	gate *pluginCallGate
}

type gatedI32ToI32I32HostFunc struct {
	fn   i32ToI32I32HostFunc
	gate *pluginCallGate
}

type gatedI32I32ToI32I32HostFunc struct {
	fn   i32I32ToI32I32HostFunc
	gate *pluginCallGate
}

type gatedI32I32ToI32HostFunc struct {
	fn   i32I32ToI32HostFunc
	gate *pluginCallGate
}

type gatedI32HostEvent struct {
	fn   I32HostEvent
	gate *pluginCallGate
}

type gatedHostCallFunc struct {
	fn   any
	gate *pluginCallGate
}

type gatedOrdinaryHostFunc struct {
	fn   any
	gate *pluginCallGate
}

func gateHostImport(value any, gate *pluginCallGate) (any, error) {
	if custom, ok := value.(nativeCustomInstructionBinding); ok {
		if custom.fn == nil {
			return nil, fmt.Errorf("native custom instruction callback is nil")
		}
		custom.fn = gate.wrap(custom.fn)
		return custom, nil
	}
	switch fn := value.(type) {
	case slotHostFunc:
		if fn == nil {
			return nil, fmt.Errorf("host function is nil")
		}
		return gate.wrap(fn), nil
	case callerSlotHostFunc:
		if fn == nil {
			return nil, fmt.Errorf("caller host function is nil")
		}
		return gate.wrapCaller(fn), nil
	case HostCallFunc:
		if fn == nil {
			return nil, fmt.Errorf("host call function is nil")
		}
		return gatedHostCallFunc{fn: fn, gate: gate}, nil
	case func(HostCall):
		if fn == nil {
			return nil, fmt.Errorf("host call function is nil")
		}
		return gatedHostCallFunc{fn: HostCallFunc(fn), gate: gate}, nil
	case CallerHostCallFunc:
		if fn == nil {
			return nil, fmt.Errorf("caller host call function is nil")
		}
		return gatedHostCallFunc{fn: fn, gate: gate}, nil
	case func(Caller, HostCall):
		if fn == nil {
			return nil, fmt.Errorf("caller host call function is nil")
		}
		return gatedHostCallFunc{fn: CallerHostCallFunc(fn), gate: gate}, nil
	case noArgsHostFunc:
		return gatedNoArgsHostFunc{fn: fn, gate: gate}, nil
	case func():
		return gatedNoArgsHostFunc{fn: noArgsHostFunc(fn), gate: gate}, nil
	case i32HostFunc:
		return gatedI32HostFunc{fn: fn, gate: gate}, nil
	case func(int32):
		return gatedI32HostFunc{fn: i32HostFunc(fn), gate: gate}, nil
	case i32ToI32HostFunc:
		return gatedI32ToI32HostFunc{fn: fn, gate: gate}, nil
	case func(int32) int32:
		return gatedI32ToI32HostFunc{fn: i32ToI32HostFunc(fn), gate: gate}, nil
	case i32I32HostFunc:
		return gatedI32I32HostFunc{fn: fn, gate: gate}, nil
	case func(int32, int32):
		return gatedI32I32HostFunc{fn: i32I32HostFunc(fn), gate: gate}, nil
	case i32I32ToI32HostFunc:
		return gatedI32I32ToI32HostFunc{fn: fn, gate: gate}, nil
	case func(int32, int32) int32:
		return gatedI32I32ToI32HostFunc{fn: i32I32ToI32HostFunc(fn), gate: gate}, nil
	case i32ToI32I32HostFunc:
		return gatedI32ToI32I32HostFunc{fn: fn, gate: gate}, nil
	case func(int32) (int32, int32):
		return gatedI32ToI32I32HostFunc{fn: i32ToI32I32HostFunc(fn), gate: gate}, nil
	case i32I32ToI32I32HostFunc:
		return gatedI32I32ToI32I32HostFunc{fn: fn, gate: gate}, nil
	case func(int32, int32) (int32, int32):
		return gatedI32I32ToI32I32HostFunc{fn: i32I32ToI32I32HostFunc(fn), gate: gate}, nil
	case func(int64) int64, func(int64, int64) int64,
		func(float32) float32, func(float32, float32) float32,
		func(float64) float64, func(float64, float64) float64,
		func(FuncRef) FuncRef, func(ExternRef) ExternRef,
		func(ExnRef) ExnRef, func(GCRef) GCRef, func(I31Ref) I31Ref:
		return gatedOrdinaryHostFunc{fn: value, gate: gate}, nil
	default:
		return nil, fmt.Errorf("unsupported host function %T; use an ordinary supported function or func(wago.HostCall)", value)
	}
}

func isGatedHostCallback(value any) bool {
	switch value.(type) {
	case gatedHostCallFunc, gatedOrdinaryHostFunc, gatedI32HostEvent,
		gatedNoArgsHostFunc, gatedI32HostFunc, gatedI32ToI32HostFunc,
		gatedI32I32HostFunc, gatedI32I32ToI32HostFunc,
		gatedI32ToI32I32HostFunc, gatedI32I32ToI32I32HostFunc:
		return true
	default:
		return false
	}
}

func isGatedCollectorHostCallback(value any) bool {
	switch value.(type) {
	case gatedHostCallFunc, gatedOrdinaryHostFunc:
		return true
	default:
		return false
	}
}

func bindGatedSyncHostImport(value any, sig FuncSig) (syncHostBinding, bool, error) {
	switch fn := value.(type) {
	case gatedI32HostEvent:
		return syncHostBinding{}, true, fmt.Errorf("deferred host event cannot be used by a module that requires synchronous host control")
	case gatedI32ToI32HostFunc:
		binding, err := bindI32ToI32HostFunc(fn.fn, sig)
		binding.setCallbackGate(fn.gate)
		return binding, true, err
	case gatedNoArgsHostFunc:
		binding, err := bindNoArgsHostFunc(fn.fn, sig)
		binding.setCallbackGate(fn.gate)
		return binding, true, err
	case gatedI32HostFunc:
		binding, err := bindI32HostFunc(fn.fn, sig)
		binding.setCallbackGate(fn.gate)
		return binding, true, err
	case gatedI32I32HostFunc:
		binding, err := bindI32I32HostFunc(fn.fn, sig)
		binding.setCallbackGate(fn.gate)
		return binding, true, err
	case gatedI32ToI32I32HostFunc:
		binding, err := bindI32ToI32I32HostFunc(fn.fn, sig)
		binding.setCallbackGate(fn.gate)
		return binding, true, err
	case gatedI32I32ToI32I32HostFunc:
		binding, err := bindI32I32ToI32I32HostFunc(fn.fn, sig)
		binding.setCallbackGate(fn.gate)
		return binding, true, err
	case gatedI32I32ToI32HostFunc:
		binding, err := bindI32I32ToI32HostFunc(fn.fn, sig)
		binding.setCallbackGate(fn.gate)
		return binding, true, err
	case gatedHostCallFunc:
		binding, err := bindSyncHostImport(fn.fn, sig)
		binding.setCallbackGate(fn.gate)
		return binding, true, err
	case gatedOrdinaryHostFunc:
		binding, err := bindSyncHostImport(fn.fn, sig)
		binding.setCallbackGate(fn.gate)
		return binding, true, err
	default:
		return syncHostBinding{}, false, nil
	}
}
