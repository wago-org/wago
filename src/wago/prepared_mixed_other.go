//go:build tinygo || (!amd64 && !arm64) || (!linux && !darwin && !windows)

package wago

import "fmt"

func (fn *WasmFunc) invokeDirectMixed([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared mixed entry is unavailable on this architecture")
}

func (fn *WasmFunc) invokeDirectMixedSession([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared mixed entry is unavailable on this architecture")
}

func (in *Instance) invokeDirectMixedEntry(uintptr, uint8, []bool, []bool, []uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared mixed entry is unavailable on this architecture")
}
