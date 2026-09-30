//go:build tinygo || (!amd64 && !arm64) || (!linux && !darwin && !windows)

package wago

import "fmt"

const preparedDirectFloatSupported = false

func (fn *WasmFunc) invokeDirectFloat([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared float entry is unavailable on this architecture")
}

func (fn *WasmFunc) invokeDirectFloatSession([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared float entry is unavailable on this architecture")
}

func (in *Instance) invokeDirectFloatEntry(uintptr, []bool, []bool, []uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared float entry is unavailable on this architecture")
}
