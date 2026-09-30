//go:build tinygo || (!amd64 && !arm64) || (!linux && !darwin && !windows)

package wago

import "fmt"

const preparedDirectWideSupported = false
const preparedDirectWideMaxArgs = 4

func (fn *WasmFunc) invokeDirectIntWide([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: wide direct prepared integer entry is unavailable on this architecture")
}

func (fn *WasmFunc) invokeDirectIntWideSession([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: wide direct prepared integer entry is unavailable on this architecture")
}

func (in *Instance) invokeDirectIntWideEntry(uintptr, []bool, []bool, []uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: wide direct prepared integer entry is unavailable on this architecture")
}
