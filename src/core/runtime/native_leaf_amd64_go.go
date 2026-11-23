// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.
//
// SPDX-License-Identifier: MPL-2.0
// Copyright (c) 2026 Wago authors and contributors

//go:build (linux || darwin || windows) && amd64 && !tinygo

package runtime

import (
	"reflect"
	goruntime "runtime"

	x64 "github.com/wago-org/wago/src/core/encoder/amd64"
)

const scalarLeafArg0, scalarLeafArg1 = x64.RAX, x64.RBX

func scalarLeafCanRegister(any, uint32) bool { return true }

func scalarLeafBody(fn any, params uint32) []byte {
	if !scalarGoABI(goruntime.Version()) || params < 1 || params > 2 {
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
	pc := value.Pointer()
	f := goruntime.FuncForPC(pc)
	if f == nil || f.Entry() != pc {
		return nil
	}
	defined := uint32(1)
	if params == 2 {
		defined |= 1 << 3
	} // ABIInternal: AX, BX
	body := make([]byte, 0, 128)
	for off := uintptr(0); off < 128; {
		var bytes [15]byte
		n := 0
		for ; n < len(bytes) && off+uintptr(n) < 128; n++ {
			at := pc + off + uintptr(n)
			owner := goruntime.FuncForPC(at)
			if owner == nil || owner.Entry() != pc {
				break
			}
			bytes[n] = *(*byte)(offHeapPointer(at))
		}
		if n == 0 {
			return nil
		}
		if bytes[0] == 0xc3 {
			return body
		}
		size, ok := scalarLeafInstruction(bytes[:n], &defined)
		if !ok || size == 0 {
			return nil
		}
		body = append(body, bytes[:size]...)
		off += uintptr(size)
	}
	return nil
}
