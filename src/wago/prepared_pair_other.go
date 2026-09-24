//go:build tinygo || (!amd64 && !arm64) || (!linux && !darwin && !windows)

package wago

import "fmt"

const preparedDirectPairSupported = false

func (in *Instance) invokeDirectIntPairEntry(uintptr, uint8, []bool, []uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer pair entry is unavailable")
}

func (fn *WasmFunc) invokeDirectIntPairSession(uint64, uint64, uint64, uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer pair entry is unavailable")
}
