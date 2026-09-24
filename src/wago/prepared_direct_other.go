//go:build (!amd64 && !arm64) || (tinygo && amd64 && !linux) || (arm64 && !(linux || darwin || windows)) || (tinygo && arm64 && windows)

package wago

import "fmt"

const preparedDirectIntSupported = false
const preparedDirectIntPrivateSupported = false
const preparedIntCallBlockDefault = false

func (fn *WasmFunc) initDirectIntCall() {}

func (in *Instance) invokeCachedDirectInt1(*invokeCache, uintptr, uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

func (in *Instance) invokeCachedDirectI32ToI32(*invokeCache, uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

func (fn *WasmFunc) invokeDirectInt([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

func (fn *WasmFunc) invokeDirectIntFixed(uint64, uint64, uint64, uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

func (in *Instance) invokeDirectIntEntry(uintptr, int, int, uint8, bool, bool, bool, bool, uint64, uint64, uint64, uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}
