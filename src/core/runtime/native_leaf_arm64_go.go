// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && arm64 && !tinygo

package runtime

import (
	"encoding/binary"
	"reflect"
	goruntime "runtime"
)

func scalarLeafCanRegister(any, uint32) bool { return true }

func scalarLeafBody(fn any, params uint32) []byte {
	if !scalarGoABI(goruntime.Version()) {
		return nil
	}
	if params < 1 || params > 2 {
		return nil
	}
	value := reflect.ValueOf(fn)
	if value.Kind() != reflect.Func || value.IsNil() {
		return nil
	}
	typ := value.Type()
	if typ.IsVariadic() || typ.NumIn() != int(params) || typ.NumOut() != 1 || typ.Out(0).Kind() != reflect.Int32 {
		return nil
	}
	for i := 0; i < typ.NumIn(); i++ {
		if typ.In(i).Kind() != reflect.Int32 {
			return nil
		}
	}
	return scalarLeafBodyAtPC(value.Pointer(), params)
}

// The caller proves the initial register inputs. Every copied instruction is
// still checked independently; no stack, memory, call or branch is accepted.
func scalarLeafBodyAtPC(pc uintptr, params uint32) []byte {
	f := goruntime.FuncForPC(pc)
	if f == nil || f.Entry() != pc {
		return nil
	}
	defined := uint32(1<<params) - 1
	body := make([]byte, 0, 128)
	for off := uintptr(0); off < 128; off += 4 {
		current := goruntime.FuncForPC(pc + off)
		if current == nil || current.Entry() != pc {
			return nil
		}
		w := *(*uint32)(offHeapPointer(pc + off))
		if w == 0xd65f03c0 { // RET LR, copied body leaves the native caller LR intact
			if defined&1 == 0 {
				return nil
			}
			return body
		}
		if !scalarLeafInstruction(w, &defined) {
			return nil
		}
		body = binary.LittleEndian.AppendUint32(body, w)
	}
	return nil
}
