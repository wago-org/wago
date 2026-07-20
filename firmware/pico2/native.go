//go:build tinygo && pico2

package main

import (
	"unsafe"

	"github.com/wago-org/wago/src/core/runtime/embedded32"
)

//go:linkname nativeInvoke wago_pico2_native_invoke
func nativeInvoke(entry uint32, payload unsafe.Pointer, stackTop uint32) uint32

//go:linkname nativePublish wago_pico2_publish
func nativePublish()

//go:linkname nativeF64Address wago_pico2_helper_f64
func nativeF64Address() uint32

//go:linkname nativeSIMDAddress wago_pico2_helper_simd
func nativeSIMDAddress() uint32

//go:linkname nativeI64Address wago_pico2_helper_i64
func nativeI64Address() uint32

//go:linkname nativeF32Address wago_pico2_helper_f32
func nativeF32Address() uint32

type boardNative struct {
	stackTop uint32
}

func (n boardNative) Start(entryAddress, contextAddress uint32) embedded32.TransportCode {
	return embedded32.TransportCode(nativeInvoke(entryAddress, unsafe.Pointer(uintptr(contextAddress)), n.stackTop))
}

func (n boardNative) Call(entryAddress, contextAddress uint32, parameters, results []uint32) embedded32.TransportCode {
	call := embedded32.CallABI{Context: contextAddress}
	if len(parameters) != 0 {
		call.Parameters = uint32(uintptr(unsafe.Pointer(&parameters[0])))
	}
	if len(results) != 0 {
		call.Results = uint32(uintptr(unsafe.Pointer(&results[0])))
	}
	return embedded32.TransportCode(nativeInvoke(entryAddress, unsafe.Pointer(&call), n.stackTop))
}

func boardHelperEntries() [4]uint32 {
	return [4]uint32{nativeF64Address(), nativeSIMDAddress(), nativeI64Address(), nativeF32Address()}
}
