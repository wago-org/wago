//go:build (!amd64 && !arm64) || (tinygo && amd64 && !linux) || (arm64 && !(linux || darwin || windows)) || (tinygo && arm64 && windows)

package wago

import "fmt"

const preparedDirectIntSupported = false
const preparedDirectIntPrivateSupported = false
const preparedIntCallBlockDefault = false
const invokeCachedPreboundIntSupported = false

type cachedInvokeIntCall struct{}

func (in *Instance) prepareCachedInvokeIntCall(*cachedInvokeIntCall, uintptr, uintptr) {}

func (in *Instance) invokeCachedPreboundInt(*invokeCache, []uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: cached prebound integer entry is unavailable")
}

func (fn *WasmFunc) initDirectIntCall() {}

func (fn *WasmFunc) invokeDirectInt([]uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

func (fn *WasmFunc) invokeDirectIntFixed(uint64, uint64, uint64, uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}

func (in *Instance) invokeDirectIntEntry(uintptr, int, int, uint8, bool, bool, bool, bool, uint64, uint64, uint64, uint64) ([]uint64, error) {
	return nil, fmt.Errorf("wago: direct prepared integer entry is unavailable on this architecture")
}
